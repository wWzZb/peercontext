package lanhost

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	protocolv2 "github.com/wWzZb/peercontext/pkg/protocol/v2"
)

type Profile struct {
	ProjectID     string
	ProjectName   string
	MemberID      string
	MemberName    string
	HostPublicKey ed25519.PublicKey
	Endpoints     []string
	Project       protocolv2.Project
	Member        protocolv2.Member
}

type DiscoverFunc func(context.Context, string, ed25519.PublicKey) ([]string, error)

type Client struct {
	Profile    Profile
	PrivateKey ed25519.PrivateKey
	HTTP       *http.Client
	Discover   DiscoverFunc
	OnEndpoint func(string)
	replays    *replayGuard
	mu         sync.Mutex
	endpoint   string
}

func NewClient(profile Profile, privateKey ed25519.PrivateKey) *Client {
	return &Client{Profile: profile, PrivateKey: privateKey, HTTP: &http.Client{Timeout: protocolv2.DefaultRequestTimeout}, replays: newReplayGuard(protocolv2.DefaultSignatureMaxAge)}
}

func Join(ctx context.Context, invitation protocolv2.Invitation, memberName string, discover DiscoverFunc) (Profile, ed25519.PrivateKey, error) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return Profile{}, nil, err
	}
	nonce, err := newNonce()
	if err != nil {
		return Profile{}, nil, err
	}
	request := protocolv2.JoinRequest{SchemaVersion: protocolv2.SchemaVersion, ProjectID: invitation.ProjectID, InviteID: invitation.InviteID, MemberName: memberName, MemberPublicKey: publicKey, Method: http.MethodPost, Path: "/v2/join", Nonce: nonce, Timestamp: time.Now().UTC()}
	request.Sign(ed25519.PrivateKey(invitation.InvitePrivateKey))
	body, _ := json.Marshal(request)
	endpoints := append([]string(nil), invitation.Endpoints...)
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		for _, endpoint := range endpoints {
			result, err := postJoin(ctx, endpoint, body, invitation.HostPublicKey, request.Nonce)
			if err != nil {
				lastErr = err
				continue
			}
			return Profile{ProjectID: result.Project.ID, ProjectName: result.Project.Name, MemberID: result.Member.ID, MemberName: result.Member.Name, HostPublicKey: invitation.HostPublicKey, Endpoints: prioritizeEndpoint(endpoint, endpoints), Project: result.Project, Member: result.Member}, privateKey, nil
		}
		if attempt == 0 && discover != nil {
			endpoints, lastErr = discover(ctx, invitation.ProjectID, invitation.HostPublicKey)
		}
	}
	if lastErr == nil {
		lastErr = errors.New("Project host is unavailable on this LAN")
	}
	return Profile{}, nil, lastErr
}

func postJoin(ctx context.Context, endpoint string, body []byte, hostPublicKey ed25519.PublicKey, replyTo string) (JoinResult, error) {
	var result JoinResult
	if err := requireDirectLANEndpoint(endpoint); err != nil {
		return result, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, normalizeEndpoint(endpoint)+"/v2/join", bytes.NewReader(body))
	if err != nil {
		return result, err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return result, err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusForbidden {
		return result, errors.New("Project host requires a directly connected LAN peer")
	}
	message, rpc, err := decodeSignedResponse(response.Body, hostPublicKey, http.MethodPost, "/v2/join", replyTo)
	if err != nil {
		return result, err
	}
	if message.Kind != "join.response" || !rpc.OK {
		return result, rpcResultError(rpc)
	}
	if err := json.Unmarshal(rpc.Data, &result); err != nil {
		return result, err
	}
	if result.Project.ID != message.ProjectID || !ed25519.PublicKey(result.Project.HostPublicKey).Equal(hostPublicKey) {
		return result, errors.New("join response does not match the invitation host")
	}
	return result, nil
}

func (c *Client) RPC(ctx context.Context, kind string, input, output any) error {
	payload, err := json.Marshal(input)
	if err != nil {
		return err
	}
	nonce, err := newNonce()
	if err != nil {
		return err
	}
	message := protocolv2.NewSignedHTTPMessage(c.Profile.ProjectID, c.Profile.MemberID, kind, http.MethodPost, "/v2/rpc", nonce, payload, time.Now().UTC(), c.PrivateKey)
	body, _ := json.Marshal(message)
	endpoints := c.endpoints()
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		for _, endpoint := range endpoints {
			rpc, responseMessage, err := c.postRPC(ctx, endpoint, body, message.Nonce)
			if err != nil {
				lastErr = err
				continue
			}
			if responseMessage.ProjectID != c.Profile.ProjectID || responseMessage.Kind != kind+".response" {
				return errors.New("host response identity does not match request")
			}
			if !rpc.OK {
				return rpcResultError(rpc)
			}
			c.rememberEndpoint(endpoint)
			if output == nil || len(rpc.Data) == 0 {
				return nil
			}
			return json.Unmarshal(rpc.Data, output)
		}
		if attempt == 0 && c.Discover != nil {
			endpoints, lastErr = c.Discover(ctx, c.Profile.ProjectID, c.Profile.HostPublicKey)
		}
	}
	if lastErr == nil {
		lastErr = errors.New("Project host is unavailable on this LAN")
	}
	return lastErr
}

func (c *Client) postRPC(ctx context.Context, endpoint string, body []byte, replyTo string) (RPCResult, protocolv2.SignedMessage, error) {
	var rpc RPCResult
	if err := requireDirectLANEndpoint(endpoint); err != nil {
		return rpc, protocolv2.SignedMessage{}, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, normalizeEndpoint(endpoint)+"/v2/rpc", bytes.NewReader(body))
	if err != nil {
		return rpc, protocolv2.SignedMessage{}, err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := c.HTTP.Do(request)
	if err != nil {
		return rpc, protocolv2.SignedMessage{}, err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusForbidden {
		return rpc, protocolv2.SignedMessage{}, errors.New("Project host requires a directly connected LAN peer")
	}
	message, rpc, err := decodeSignedResponse(response.Body, c.Profile.HostPublicKey, http.MethodPost, "/v2/rpc", replyTo)
	if err == nil {
		err = c.replays.Accept(message, time.Now().UTC())
	}
	return rpc, message, err
}

func decodeSignedResponse(reader io.Reader, hostPublicKey ed25519.PublicKey, method, path, replyTo string) (protocolv2.SignedMessage, RPCResult, error) {
	var message protocolv2.SignedMessage
	var rpc RPCResult
	if err := decodeJSON(reader, &message, protocolv2.MaxWireMessageBytes); err != nil {
		return message, rpc, err
	}
	if err := message.Verify(hostPublicKey, time.Now().UTC(), protocolv2.DefaultSignatureMaxAge); err != nil {
		return message, rpc, fmt.Errorf("verify host response: %w", err)
	}
	if message.Method != method || message.Path != path {
		return message, rpc, errors.New("host response route does not match request")
	}
	if message.ReplyTo != replyTo {
		return message, rpc, errors.New("host response does not match the current request")
	}
	if err := json.Unmarshal(message.Payload, &rpc); err != nil {
		return message, rpc, err
	}
	return message, rpc, nil
}

func (c *Client) CreateInvitation(ctx context.Context) (protocolv2.Invitation, error) {
	var invitation protocolv2.Invitation
	err := c.RPC(ctx, KindInviteCreate, InviteCreateInput{}, &invitation)
	return invitation, err
}

func (c *Client) ListAgents(ctx context.Context) ([]protocolv2.Agent, error) {
	var agents []protocolv2.Agent
	err := c.RPC(ctx, KindAgentsList, struct{}{}, &agents)
	return agents, err
}

func (c *Client) Ask(ctx context.Context, request protocolv2.Request) (protocolv2.ProviderPayload, error) {
	var response protocolv2.ProviderPayload
	err := c.RPC(ctx, KindRequestAsk, request, &response)
	return response, err
}

type ProviderHandler func(context.Context, protocolv2.Request) (protocolv2.Response, *protocolv2.RequestFailure)

func (c *Client) RunProvider(ctx context.Context, agentID string, handler ProviderHandler) error {
	endpoints := c.endpoints()
	var lastErr error
	for _, endpoint := range endpoints {
		err := c.runProviderEndpoint(ctx, endpoint, agentID, handler)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = ErrAgentOffline
	}
	return lastErr
}

func (c *Client) runProviderEndpoint(ctx context.Context, endpoint, agentID string, handler ProviderHandler) error {
	if err := requireDirectLANEndpoint(endpoint); err != nil {
		return err
	}
	payload, _ := json.Marshal(ProviderConnectInput{AgentID: agentID})
	nonce, err := newNonce()
	if err != nil {
		return err
	}
	auth := protocolv2.NewSignedHTTPMessage(c.Profile.ProjectID, c.Profile.MemberID, KindProviderConnect, http.MethodGet, "/v2/provider", nonce, payload, time.Now().UTC(), c.PrivateKey)
	authJSON, _ := json.Marshal(auth)
	parsed, err := url.Parse(normalizeEndpoint(endpoint) + "/v2/provider")
	if err != nil {
		return err
	}
	parsed.Scheme = "ws"
	header := http.Header{"X-Peerctx-Auth": []string{base64.RawURLEncoding.EncodeToString(authJSON)}}
	conn, handshake, err := websocket.DefaultDialer.DialContext(ctx, parsed.String(), header)
	if err != nil {
		return err
	}
	defer conn.Close()
	conn.SetReadLimit(protocolv2.MaxWireMessageBytes)
	connectedJSON, err := base64.RawURLEncoding.DecodeString(handshake.Header.Get("X-Peerctx-Host-Auth"))
	if err != nil {
		return errors.New("Project host did not sign the provider connection")
	}
	var connected protocolv2.SignedMessage
	if err := json.Unmarshal(connectedJSON, &connected); err != nil || connected.ProjectID != c.Profile.ProjectID || connected.Kind != "provider.connected" || connected.Method != http.MethodGet || connected.Path != "/v2/provider" || connected.ReplyTo != auth.Nonce || connected.Verify(c.Profile.HostPublicKey, time.Now().UTC(), protocolv2.DefaultSignatureMaxAge) != nil {
		return errors.New("Project host provider signature is invalid")
	}
	if err := c.replays.Accept(connected, time.Now().UTC()); err != nil {
		return err
	}
	c.rememberEndpoint(endpoint)
	for {
		var message protocolv2.SignedMessage
		if err := conn.ReadJSON(&message); err != nil {
			return err
		}
		if err := message.Verify(c.Profile.HostPublicKey, time.Now().UTC(), protocolv2.DefaultSignatureMaxAge); err != nil {
			return err
		}
		if message.Method != "WS" || message.Path != "/v2/provider" {
			return errors.New("Project host frame route is invalid")
		}
		if err := c.replays.Accept(message, time.Now().UTC()); err != nil {
			return err
		}
		var incoming protocolv2.ProviderPayload
		if err := json.Unmarshal(message.Payload, &incoming); err != nil || incoming.Type != protocolv2.ProviderRequest || incoming.Request == nil {
			return errors.New("invalid request from Project host")
		}
		response, failure := handler(ctx, *incoming.Request)
		if failure == nil && len(response.Answer) > protocolv2.MaxResponseBodyBytes {
			failure = &protocolv2.RequestFailure{SchemaVersion: protocolv2.SchemaVersion, RequestID: incoming.RequestID, Code: "response_too_large", Message: "Agent response exceeds 2 MiB", Retryable: false}
		}
		outgoing := protocolv2.ProviderPayload{SchemaVersion: protocolv2.SchemaVersion, AgentID: agentID, RequestID: incoming.RequestID}
		if failure != nil {
			outgoing.Type = protocolv2.ProviderFailure
			outgoing.Failure = failure
		} else {
			outgoing.Type = protocolv2.ProviderResponse
			outgoing.Response = &response
		}
		outgoingJSON, _ := json.Marshal(outgoing)
		nonce, err := newNonce()
		if err != nil {
			return err
		}
		signed := protocolv2.NewSignedReply(c.Profile.ProjectID, c.Profile.MemberID, string(outgoing.Type), "WS", "/v2/provider", message.Nonce, nonce, outgoingJSON, time.Now().UTC(), c.PrivateKey)
		if err := conn.WriteJSON(signed); err != nil {
			return err
		}
	}
}

func (c *Client) endpoints() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.endpoint == "" {
		return append([]string(nil), c.Profile.Endpoints...)
	}
	return prioritizeEndpoint(c.endpoint, c.Profile.Endpoints)
}

func (c *Client) rememberEndpoint(endpoint string) {
	c.mu.Lock()
	changed := c.endpoint != endpoint && (len(c.Profile.Endpoints) == 0 || c.Profile.Endpoints[0] != endpoint)
	c.endpoint = endpoint
	c.mu.Unlock()
	if changed && c.OnEndpoint != nil {
		c.OnEndpoint(endpoint)
	}
}

func prioritizeEndpoint(first string, endpoints []string) []string {
	result := []string{first}
	for _, endpoint := range endpoints {
		if endpoint != first {
			result = append(result, endpoint)
		}
	}
	return result
}

func rpcResultError(result RPCResult) error {
	if result.Error == nil {
		return errors.New("PeerContext host rejected the request")
	}
	return fmt.Errorf("%s: %s", result.Error.Code, result.Error.Message)
}

func requireDirectLANEndpoint(endpoint string) error {
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Scheme != "http" {
		return errors.New("Project endpoint is invalid")
	}
	host := parsed.Hostname()
	port := parsed.Port()
	if port == "" {
		port = "80"
	}
	if net.ParseIP(host) == nil || !isDirectLANRemote(net.JoinHostPort(host, port)) {
		return errors.New("Project endpoint is not on a directly connected LAN")
	}
	return nil
}
