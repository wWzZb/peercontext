package lanhost

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/wWzZb/peercontext/internal/failure"
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
				if terminalEndpointError(err) {
					return Profile{}, nil, err
				}
				lastErr = err
				continue
			}
			return Profile{ProjectID: result.Project.ID, ProjectName: result.Project.Name, MemberID: result.Member.ID, MemberName: result.Member.Name, HostPublicKey: invitation.HostPublicKey, Endpoints: prioritizeEndpoint(endpoint, endpoints), Project: result.Project, Member: result.Member}, privateKey, nil
		}
		if ctx.Err() != nil {
			return Profile{}, nil, ctx.Err()
		}
		if attempt == 0 && discover != nil {
			endpoints, lastErr = discover(ctx, invitation.ProjectID, invitation.HostPublicKey)
		}
	}
	if lastErr == nil {
		lastErr = failure.New("project_host_offline", "The Project host is offline or unreachable on this LAN.", true)
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
	message, rpc, err := decodeSignedResponse(response.Body, hostPublicKey, http.MethodPost, "/v2/join", replyTo)
	if err != nil {
		if response.StatusCode == http.StatusForbidden {
			if terminalEndpointError(err) {
				return result, err
			}
			return result, failure.New("lan_peer_required", "The Project host only accepts directly connected LAN peers.", false)
		}
		return result, err
	}
	if message.Kind != "join.response" || !rpc.OK {
		return result, rpcResultError(rpc)
	}
	if err := json.Unmarshal(rpc.Data, &result); err != nil {
		return result, err
	}
	if result.Project.ID != message.ProjectID || !ed25519.PublicKey(result.Project.HostPublicKey).Equal(hostPublicKey) {
		return result, failure.New("host_identity_mismatch", "The responding host does not match the invitation.", false)
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
				if terminalEndpointError(err) {
					return err
				}
				lastErr = err
				continue
			}
			if responseMessage.ProjectID != c.Profile.ProjectID || responseMessage.Kind != kind+".response" {
				return failure.New("host_identity_mismatch", "The Project host response does not match the request.", false)
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
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if attempt == 0 && c.Discover != nil {
			endpoints, lastErr = c.Discover(ctx, c.Profile.ProjectID, c.Profile.HostPublicKey)
		}
	}
	if lastErr == nil {
		lastErr = failure.New("project_host_offline", "The Project host is offline or unreachable on this LAN.", true)
	}
	return lastErr
}

func terminalEndpointError(err error) bool {
	var structured *failure.Error
	if !errors.As(err, &structured) {
		return false
	}
	return structured.Code != "project_host_offline" && structured.Code != "lan_discovery_unavailable"
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
	message, rpc, err := decodeSignedResponse(response.Body, c.Profile.HostPublicKey, http.MethodPost, "/v2/rpc", replyTo)
	if err != nil && response.StatusCode == http.StatusForbidden {
		if terminalEndpointError(err) {
			return rpc, protocolv2.SignedMessage{}, err
		}
		return rpc, protocolv2.SignedMessage{}, failure.New("lan_peer_required", "The Project host only accepts directly connected LAN peers.", false)
	}
	if err == nil {
		err = c.replays.Accept(message, time.Now().UTC())
		if errors.Is(err, ErrRequestReplayed) {
			err = failure.Wrap("request_replayed", "The Project host response nonce was already used.", false, err)
		}
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
		if errors.Is(err, protocolv2.ErrClockSkew) {
			return message, rpc, failure.Wrap("clock_skew", "The Project host clock differs too much from this Mac.", false, err)
		}
		return message, rpc, failure.Wrap("host_identity_mismatch", "The Project host response could not be verified with the saved host key.", false, err)
	}
	if message.Method != method || message.Path != path {
		return message, rpc, failure.New("host_identity_mismatch", "The Project host response does not match the request route.", false)
	}
	if message.ReplyTo != replyTo {
		return message, rpc, failure.New("host_identity_mismatch", "The Project host response does not match the current request.", false)
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
	return c.RunProviderWithState(ctx, agentID, handler, nil)
}

func (c *Client) RunProviderWithState(ctx context.Context, agentID string, handler ProviderHandler, onState func(bool)) error {
	endpoints := c.endpoints()
	var lastErr error
	for _, endpoint := range endpoints {
		err := c.runProviderEndpoint(ctx, endpoint, agentID, handler, onState)
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

func (c *Client) runProviderEndpoint(ctx context.Context, endpoint, agentID string, handler ProviderHandler, onState func(bool)) error {
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
	watchDone := make(chan struct{})
	defer close(watchDone)
	go func() {
		select {
		case <-ctx.Done():
			_ = conn.Close()
		case <-watchDone:
		}
	}()
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
	if onState != nil {
		onState(true)
		defer onState(false)
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
	return failure.New(result.Error.Code, result.Error.Message, result.Error.Retryable)
}

func requireDirectLANEndpoint(endpoint string) error {
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Scheme != "http" {
		return failure.New("invalid_invitation", "The Project endpoint in the invitation is invalid.", false)
	}
	host := parsed.Hostname()
	port := parsed.Port()
	if port == "" {
		port = "80"
	}
	if net.ParseIP(host) == nil || !isDirectLANRemote(net.JoinHostPort(host, port)) {
		return failure.New("lan_peer_required", "The Project endpoint is not on a directly connected LAN.", false)
	}
	return nil
}
