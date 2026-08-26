// Package relayclient implements the public Relay HTTP and WebSocket client.
// It never logs request/response bodies or credentials.
package relayclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/wWzZb/peercontext/internal/relay"
	protocolv1 "github.com/wWzZb/peercontext/pkg/protocol/v1"
)

type Error struct {
	Status    int
	Code      string
	Message   string
	Retryable bool
}

func (e *Error) Error() string {
	return fmt.Sprintf("Relay error %s (%d): %s", e.Code, e.Status, e.Message)
}

type TransportError struct{ Err error }

func (e *TransportError) Error() string { return "Relay transport failed: " + e.Err.Error() }
func (e *TransportError) Unwrap() error { return e.Err }

type Client struct {
	baseURL    string
	token      string
	httpClient *http.Client
}

func New(baseURL, token string) (*Client, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, errors.New("relay URL must be an absolute http:// or https:// URL")
	}
	return &Client{baseURL: baseURL, token: token, httpClient: &http.Client{}}, nil
}

func (c *Client) Health(ctx context.Context) error {
	var result struct {
		Status string `json:"status"`
	}
	if err := c.do(ctx, http.MethodGet, "/healthz", nil, &result, false); err != nil {
		return err
	}
	if result.Status != "ok" {
		return errors.New("Relay health response is invalid")
	}
	return nil
}

type ProjectSession struct {
	SchemaVersion   int           `json:"schema_version"`
	Project         relay.Project `json:"project"`
	Member          relay.Member  `json:"member"`
	CredentialToken string        `json:"credential_token"`
}

func (c *Client) CreateProject(ctx context.Context, name, owner string) (ProjectSession, error) {
	var result ProjectSession
	err := c.do(ctx, http.MethodPost, "/v1/projects", map[string]any{"name": name, "owner_name": owner}, &result, false)
	return result, err
}
func (c *Client) JoinProject(ctx context.Context, inviteToken, member string) (ProjectSession, error) {
	var result ProjectSession
	err := c.do(ctx, http.MethodPost, "/v1/project/join", map[string]any{"invite_token": inviteToken, "member_name": member}, &result, false)
	return result, err
}

type InviteResult struct {
	SchemaVersion int          `json:"schema_version"`
	Invite        relay.Invite `json:"invite"`
	InviteToken   string       `json:"invite_token"`
}

func (c *Client) CreateInvite(ctx context.Context, ttl time.Duration) (InviteResult, error) {
	var result InviteResult
	err := c.do(ctx, http.MethodPost, "/v1/project/invites", map[string]any{"expires_in_seconds": int64(ttl / time.Second)}, &result, true)
	return result, err
}
func (c *Client) ListMembers(ctx context.Context) ([]relay.Member, error) {
	var result struct {
		Members []relay.Member `json:"members"`
	}
	err := c.do(ctx, http.MethodGet, "/v1/project/members", nil, &result, true)
	return result.Members, err
}
func (c *Client) PromoteMember(ctx context.Context, id string) (relay.Member, error) {
	var result relay.Member
	err := c.do(ctx, http.MethodPost, "/v1/project/members/"+url.PathEscape(id)+"/promote", map[string]any{}, &result, true)
	return result, err
}
func (c *Client) RemoveMember(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/v1/project/members/"+url.PathEscape(id), nil, nil, true)
}

type CredentialStatus struct {
	SchemaVersion int              `json:"schema_version"`
	Project       relay.Project    `json:"project"`
	Member        relay.Member     `json:"member"`
	Credential    relay.Credential `json:"credential"`
}

func (c *Client) CredentialStatus(ctx context.Context) (CredentialStatus, error) {
	var result CredentialStatus
	err := c.do(ctx, http.MethodGet, "/v1/credential/status", nil, &result, true)
	return result, err
}

type RotateResult struct {
	SchemaVersion   int              `json:"schema_version"`
	Credential      relay.Credential `json:"credential"`
	CredentialToken string           `json:"credential_token"`
}

func (c *Client) RotateCredential(ctx context.Context) (RotateResult, error) {
	var result RotateResult
	err := c.do(ctx, http.MethodPost, "/v1/credential/rotate", map[string]any{}, &result, true)
	return result, err
}
func (c *Client) RevokeCredential(ctx context.Context, id string) error {
	if id == "" {
		return c.do(ctx, http.MethodDelete, "/v1/credential", nil, nil, true)
	}
	return c.do(ctx, http.MethodDelete, "/v1/credentials/"+url.PathEscape(id), nil, nil, true)
}

func (c *Client) RegisterAgent(ctx context.Context, manifest protocolv1.AgentManifest) (relay.Agent, error) {
	var result relay.Agent
	err := c.do(ctx, http.MethodPost, "/v1/agents", manifest, &result, true)
	return result, err
}
func (c *Client) ListAgents(ctx context.Context) ([]relay.Agent, error) {
	var result struct {
		Agents []relay.Agent `json:"agents"`
	}
	err := c.do(ctx, http.MethodGet, "/v1/agents", nil, &result, true)
	return result.Agents, err
}
func (c *Client) GetAgent(ctx context.Context, id string) (relay.Agent, error) {
	var result relay.Agent
	err := c.do(ctx, http.MethodGet, "/v1/agents/"+url.PathEscape(id), nil, &result, true)
	return result, err
}
func (c *Client) SetAccess(ctx context.Context, agentID, memberID string, modes []protocolv1.RequestMode, grant bool) (relay.ACL, error) {
	var result relay.ACL
	err := c.do(ctx, http.MethodPost, "/v1/agents/"+url.PathEscape(agentID)+"/access", map[string]any{"member_id": memberID, "modes": modes, "grant": grant}, &result, true)
	return result, err
}

func (c *Client) Ask(ctx context.Context, request protocolv1.Request) (protocolv1.AskResult, error) {
	var result protocolv1.AskResult
	err := c.do(ctx, http.MethodPost, "/v1/requests", request, &result, true)
	return result, err
}

// Submit sends either a read request or a requester-confirmed write request.
// The client does not infer mode from Body.
func (c *Client) Submit(ctx context.Context, request protocolv1.Request) (protocolv1.AskResult, error) {
	return c.Ask(ctx, request)
}

func (c *Client) GetRequest(ctx context.Context, requestID string) (protocolv1.RequestMetadata, error) {
	var result protocolv1.RequestMetadata
	err := c.do(ctx, http.MethodGet, "/v1/requests/"+url.PathEscape(requestID), nil, &result, true)
	return result, err
}

func (c *Client) CancelRequest(ctx context.Context, requestID string) (protocolv1.RequestMetadata, error) {
	var result protocolv1.RequestMetadata
	err := c.do(ctx, http.MethodDelete, "/v1/requests/"+url.PathEscape(requestID), nil, &result, true)
	return result, err
}

func (c *Client) PendingRequests(ctx context.Context) ([]protocolv1.PendingRequest, error) {
	var result struct {
		Requests []protocolv1.PendingRequest `json:"requests"`
	}
	err := c.do(ctx, http.MethodGet, "/v1/requests/pending", nil, &result, true)
	return result.Requests, err
}

func (c *Client) ApproveRequest(ctx context.Context, requestID, baseCommit string) (protocolv1.RequestMetadata, error) {
	var result protocolv1.RequestMetadata
	err := c.do(ctx, http.MethodPost, "/v1/requests/"+url.PathEscape(requestID)+"/approve", map[string]any{"base_commit": baseCommit}, &result, true)
	return result, err
}

func (c *Client) DenyRequest(ctx context.Context, requestID string) (protocolv1.RequestMetadata, error) {
	var result protocolv1.RequestMetadata
	err := c.do(ctx, http.MethodPost, "/v1/requests/"+url.PathEscape(requestID)+"/deny", map[string]any{}, &result, true)
	return result, err
}

type AgentRequestHandler func(context.Context, protocolv1.Request) (protocolv1.Response, *protocolv1.RequestFailure)
type AgentJobHandler func(context.Context, protocolv1.Request, string) (protocolv1.Response, *protocolv1.RequestFailure)

func (c *Client) ServeAgent(ctx context.Context, agentID string, ready func(map[string]any)) error {
	return c.ServeAgentWithJobHandler(ctx, agentID, ready, nil)
}

func (c *Client) ServeAgentWithHandler(ctx context.Context, agentID string, ready func(map[string]any), handler AgentRequestHandler) error {
	var jobHandler AgentJobHandler
	if handler != nil {
		jobHandler = func(ctx context.Context, request protocolv1.Request, _ string) (protocolv1.Response, *protocolv1.RequestFailure) {
			return handler(ctx, request)
		}
	}
	return c.ServeAgentWithJobHandler(ctx, agentID, ready, jobHandler)
}

func (c *Client) ServeAgentWithJobHandler(ctx context.Context, agentID string, ready func(map[string]any), handler AgentJobHandler) error {
	parsed, _ := url.Parse(c.baseURL)
	if parsed.Scheme == "https" {
		parsed.Scheme = "wss"
	} else {
		parsed.Scheme = "ws"
	}
	parsed.Path = "/v1/agents/" + url.PathEscape(agentID) + "/serve"
	headers := http.Header{"Authorization": []string{"Bearer " + c.token}}
	conn, response, err := websocket.DefaultDialer.DialContext(ctx, parsed.String(), headers)
	if err != nil {
		if response != nil {
			return decodeWebSocketError(response)
		}
		return &TransportError{Err: err}
	}
	defer conn.Close()
	var first protocolv1.ProviderMessage
	if err = conn.ReadJSON(&first); err != nil {
		return err
	}
	if first.Type != protocolv1.ProviderReady {
		if first.Failure != nil {
			return &Error{Status: http.StatusConflict, Code: first.Failure.Code, Message: first.Failure.Message, Retryable: first.Failure.Retryable}
		}
		return &Error{Status: http.StatusBadGateway, Code: "provider_protocol_error", Message: "Relay did not send a valid Agent ready message."}
	}
	if err := first.Validate(); err != nil {
		return &Error{Status: http.StatusBadGateway, Code: "provider_protocol_error", Message: "Relay sent an invalid Agent ready message."}
	}
	if ready != nil {
		ready(map[string]any{"schema_version": first.SchemaVersion, "type": string(first.Type), "agent_id": first.AgentID, "runtime_mode": string(first.RuntimeMode)})
	}
	var writeMu sync.Mutex
	writeMessage := func(message protocolv1.ProviderMessage) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		return conn.WriteJSON(message)
	}
	var activeMu sync.Mutex
	active := make(map[string]context.CancelFunc)
	cancelAll := func() {
		activeMu.Lock()
		defer activeMu.Unlock()
		for _, cancel := range active {
			cancel()
		}
	}
	defer cancelAll()
	done := make(chan error, 1)
	go func() {
		for {
			var message protocolv1.ProviderMessage
			if err := conn.ReadJSON(&message); err != nil {
				done <- err
				return
			}
			switch message.Type {
			case protocolv1.ProviderRequest:
				if message.Request == nil {
					continue
				}
				if err := message.Validate(); err != nil {
					requestID := message.Request.ID
					_ = writeMessage(protocolv1.ProviderMessage{SchemaVersion: 1, Type: protocolv1.ProviderFailure, Failure: &protocolv1.RequestFailure{SchemaVersion: 1, RequestID: requestID, Code: "provider_protocol_error", Message: "Relay sent an invalid provider request.", Retryable: false}})
					continue
				}
				request := *message.Request
				baseCommit := message.BaseCommit
				requestCtx, cancel := context.WithCancel(ctx)
				activeMu.Lock()
				active[request.ID] = cancel
				activeMu.Unlock()
				go func() {
					defer func() { activeMu.Lock(); delete(active, request.ID); activeMu.Unlock(); cancel() }()
					if handler == nil {
						_ = writeMessage(protocolv1.ProviderMessage{SchemaVersion: 1, Type: protocolv1.ProviderFailure, Failure: &protocolv1.RequestFailure{SchemaVersion: 1, RequestID: request.ID, Code: "provider_runtime_unavailable", Message: "The provider runtime is unavailable.", Retryable: true}})
						return
					}
					response, failure := handler(requestCtx, request, baseCommit)
					if failure != nil {
						_ = writeMessage(protocolv1.ProviderMessage{SchemaVersion: 1, Type: protocolv1.ProviderFailure, Failure: failure})
						return
					}
					_ = writeMessage(protocolv1.ProviderMessage{SchemaVersion: 1, Type: protocolv1.ProviderResponse, Response: &response})
				}()
			case protocolv1.ProviderCancel:
				activeMu.Lock()
				cancel := active[message.RequestID]
				activeMu.Unlock()
				if cancel != nil {
					cancel()
				}
			}
		}
	}()
	ticker := time.NewTicker(20 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			_ = conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""), time.Now().Add(time.Second))
			return nil
		case err := <-done:
			return err
		case <-ticker.C:
			if err := writeMessage(protocolv1.ProviderMessage{SchemaVersion: 1, Type: protocolv1.ProviderPing}); err != nil {
				return err
			}
		}
	}
}

func (c *Client) do(ctx context.Context, method, path string, input, output any, auth bool) error {
	var body io.Reader
	if input != nil {
		encoded, err := json.Marshal(input)
		if err != nil {
			return err
		}
		body = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "application/json")
	if input != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if auth {
		request.Header.Set("Authorization", "Bearer "+c.token)
	}
	response, err := c.httpClient.Do(request)
	if err != nil {
		return &TransportError{Err: err}
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return decodeError(response)
	}
	if output == nil {
		_, _ = io.Copy(io.Discard, response.Body)
		return nil
	}
	return json.NewDecoder(io.LimitReader(response.Body, 4<<20)).Decode(output)
}
func decodeError(response *http.Response) error {
	var envelope struct {
		Error struct {
			Code      string `json:"code"`
			Message   string `json:"message"`
			Retryable bool   `json:"retryable"`
		} `json:"error"`
	}
	_ = json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&envelope)
	return &Error{Status: response.StatusCode, Code: envelope.Error.Code, Message: envelope.Error.Message, Retryable: envelope.Error.Retryable}
}
func decodeWebSocketError(response *http.Response) error {
	defer response.Body.Close()
	return decodeError(response)
}
