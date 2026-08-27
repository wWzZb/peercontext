package lanhost

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	protocolv2 "github.com/wWzZb/peercontext/pkg/protocol/v2"
)

var ErrAgentOffline = errors.New("agent is offline")

type providerConnection struct {
	projectID string
	agentID   string
	memberID  string
	key       ed25519.PublicKey
	conn      *websocket.Conn
	writeMu   sync.Mutex
}

type broker struct {
	mu        sync.Mutex
	providers map[string]*providerConnection
	pending   map[string]pendingRequest
}

type pendingRequest struct {
	provider string
	replyTo  string
	result   chan protocolv2.ProviderPayload
}

func newBroker() *broker {
	return &broker{providers: map[string]*providerConnection{}, pending: map[string]pendingRequest{}}
}

func providerKey(projectID, agentID string) string { return projectID + "\x00" + agentID }

func (b *broker) attach(provider *providerConnection) (replaced *providerConnection) {
	b.mu.Lock()
	defer b.mu.Unlock()
	key := providerKey(provider.projectID, provider.agentID)
	replaced = b.providers[key]
	b.providers[key] = provider
	return replaced
}

func (b *broker) detach(provider *providerConnection) {
	b.mu.Lock()
	defer b.mu.Unlock()
	key := providerKey(provider.projectID, provider.agentID)
	if b.providers[key] == provider {
		delete(b.providers, key)
		for requestID, pending := range b.pending {
			if pending.provider == key {
				failure := protocolv2.ProviderPayload{SchemaVersion: protocolv2.SchemaVersion, Type: protocolv2.ProviderFailure, AgentID: provider.agentID, RequestID: requestID, Failure: &protocolv2.RequestFailure{SchemaVersion: protocolv2.SchemaVersion, RequestID: requestID, Code: "agent_offline", Message: "Agent disconnected while handling the request", Retryable: true}}
				select {
				case pending.result <- failure:
				default:
				}
			}
		}
	}
}

func (b *broker) dispatch(ctx context.Context, request protocolv2.Request, identity HostIdentity) (protocolv2.ProviderPayload, error) {
	b.mu.Lock()
	provider := b.providers[providerKey(request.ProjectID, request.AgentID)]
	if provider == nil {
		b.mu.Unlock()
		return protocolv2.ProviderPayload{}, ErrAgentOffline
	}
	result := make(chan protocolv2.ProviderPayload, 1)
	payload, err := json.Marshal(protocolv2.ProviderPayload{SchemaVersion: protocolv2.SchemaVersion, Type: protocolv2.ProviderRequest, AgentID: request.AgentID, RequestID: request.ID, Request: &request})
	if err != nil {
		b.mu.Unlock()
		return protocolv2.ProviderPayload{}, err
	}
	nonce, err := newNonce()
	if err != nil {
		b.mu.Unlock()
		return protocolv2.ProviderPayload{}, err
	}
	b.pending[request.ID] = pendingRequest{provider: providerKey(request.ProjectID, request.AgentID), replyTo: nonce, result: result}
	b.mu.Unlock()
	defer func() {
		b.mu.Lock()
		delete(b.pending, request.ID)
		b.mu.Unlock()
	}()
	message := protocolv2.NewSignedHTTPMessage(request.ProjectID, identity.MemberID, string(protocolv2.ProviderRequest), "WS", "/v2/provider", nonce, payload, time.Now().UTC(), ed25519.PrivateKey(identity.PrivateKey))
	provider.writeMu.Lock()
	err = provider.conn.WriteJSON(message)
	provider.writeMu.Unlock()
	if err != nil {
		return protocolv2.ProviderPayload{}, err
	}
	select {
	case <-ctx.Done():
		return protocolv2.ProviderPayload{}, ctx.Err()
	case response := <-result:
		return response, nil
	}
}

func (b *broker) receive(message protocolv2.SignedMessage, provider *providerConnection) error {
	if message.ProjectID != provider.projectID || message.SenderID != provider.memberID {
		return errors.New("provider identity does not match connection")
	}
	if message.Method != "WS" || message.Path != "/v2/provider" {
		return errors.New("provider message route does not match connection")
	}
	if err := message.Verify(provider.key, time.Now().UTC(), protocolv2.DefaultSignatureMaxAge); err != nil {
		return err
	}
	var payload protocolv2.ProviderPayload
	if err := json.Unmarshal(message.Payload, &payload); err != nil {
		return err
	}
	if payload.Type != protocolv2.ProviderResponse && payload.Type != protocolv2.ProviderFailure {
		return errors.New("unexpected provider message")
	}
	if payload.Type == protocolv2.ProviderResponse && (payload.Response == nil || payload.Response.RequestID != payload.RequestID || len(payload.Response.Answer) > protocolv2.MaxResponseBodyBytes) {
		payload.Type = protocolv2.ProviderFailure
		payload.Response = nil
		payload.Failure = &protocolv2.RequestFailure{SchemaVersion: protocolv2.SchemaVersion, RequestID: payload.RequestID, Code: "provider_protocol_error", Message: "provider response is invalid", Retryable: false}
	}
	b.mu.Lock()
	pending := b.pending[payload.RequestID]
	b.mu.Unlock()
	if pending.replyTo == "" || message.ReplyTo != pending.replyTo {
		return errors.New("provider response does not match the active request")
	}
	if pending.result != nil {
		select {
		case pending.result <- payload:
		default:
		}
	}
	return nil
}
