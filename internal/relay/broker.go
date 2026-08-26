package relay

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	protocolv1 "github.com/wWzZb/peercontext/pkg/protocol/v1"
)

var ErrAgentOffline = errors.New("agent offline")
var ErrAgentAlreadyServing = errors.New("agent is already serving")
var ErrRequestNotPending = errors.New("request is not pending approval")
var ErrApprovalExpired = errors.New("write approval expired")

type requestOutcome struct {
	response *protocolv1.Response
	failure  *protocolv1.RequestFailure
}

type activeRequest struct {
	mu              sync.Mutex
	metadata        protocolv1.RequestMetadata
	pendingRequest  *protocolv1.Request
	approvalExpires time.Time
	approved        bool
	started         bool
	terminal        bool
	done            chan struct{}
	once            sync.Once
	outcome         requestOutcome
}

func (a *activeRequest) wait(ctx context.Context) (requestOutcome, error) {
	select {
	case <-a.done:
		return a.outcome, nil
	case <-ctx.Done():
		return requestOutcome{}, ctx.Err()
	}
}

func (a *activeRequest) metadataSnapshot() protocolv1.RequestMetadata {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.metadata
}

func (a *activeRequest) pendingSnapshot(now time.Time) (protocolv1.PendingRequest, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.terminal || a.pendingRequest == nil || a.approved || a.metadata.Status != protocolv1.StatusPendingApproval || !a.approvalExpires.After(now) {
		return protocolv1.PendingRequest{}, false
	}
	return protocolv1.PendingRequest{SchemaVersion: 1, Metadata: a.metadata, ExpiresAt: a.approvalExpires}, true
}

func (a *activeRequest) pendingState() (protocolv1.PendingRequest, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.terminal || a.pendingRequest == nil || a.approved || a.metadata.Status != protocolv1.StatusPendingApproval {
		return protocolv1.PendingRequest{}, false
	}
	return protocolv1.PendingRequest{SchemaVersion: 1, Metadata: a.metadata, ExpiresAt: a.approvalExpires}, true
}

func (a *activeRequest) claimPendingTerminal(status protocolv1.RequestStatus, now time.Time) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.terminal || a.pendingRequest == nil || a.approved || a.metadata.Status != protocolv1.StatusPendingApproval {
		return false
	}
	a.terminal = true
	a.pendingRequest = nil
	a.metadata.Status = status
	a.metadata.UpdatedAt = now
	return true
}

func (a *activeRequest) takePending(now time.Time) (protocolv1.Request, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.terminal || a.pendingRequest == nil || a.approved || a.metadata.Status != protocolv1.StatusPendingApproval {
		return protocolv1.Request{}, ErrRequestNotPending
	}
	if !a.approvalExpires.After(now) {
		a.terminal = true
		a.pendingRequest = nil
		a.metadata.Status = protocolv1.StatusExpired
		a.metadata.UpdatedAt = now
		return protocolv1.Request{}, ErrApprovalExpired
	}
	request := *a.pendingRequest
	request.Body = append([]byte(nil), a.pendingRequest.Body...)
	a.pendingRequest = nil
	a.approved = true
	a.started = true
	a.metadata.Status = protocolv1.StatusRunning
	a.metadata.UpdatedAt = now
	return request, nil
}

func (a *activeRequest) providerStarted() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.started
}

type providerSession struct {
	agentID      string
	memberID     string
	credentialID string
	conn         *websocket.Conn
	writeMu      sync.Mutex
}

func (p *providerSession) send(message protocolv1.ProviderMessage) error {
	p.writeMu.Lock()
	defer p.writeMu.Unlock()
	return p.conn.WriteJSON(message)
}

type broker struct {
	mu        sync.RWMutex
	providers map[string]*providerSession
	active    map[string]*activeRequest
	store     *Store
	now       func() time.Time
}

func newBroker(store *Store) *broker {
	return &broker{providers: make(map[string]*providerSession), active: make(map[string]*activeRequest), store: store, now: func() time.Time { return time.Now().UTC() }}
}

func (b *broker) registerProvider(agentID string, principal Principal, conn *websocket.Conn) (*providerSession, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, exists := b.providers[agentID]; exists {
		return nil, ErrAgentAlreadyServing
	}
	session := &providerSession{agentID: agentID, memberID: principal.Member.ID, credentialID: principal.Credential.ID, conn: conn}
	b.providers[agentID] = session
	return session, nil
}

func (b *broker) unregisterProvider(session *providerSession) {
	b.mu.Lock()
	if b.providers[session.agentID] == session {
		delete(b.providers, session.agentID)
	}
	var affected []*activeRequest
	for _, active := range b.active {
		if active.metadataSnapshot().AgentID == session.agentID {
			affected = append(affected, active)
		}
	}
	b.mu.Unlock()
	for _, active := range affected {
		metadata := active.metadataSnapshot()
		b.finish(active, protocolv1.StatusFailed, requestOutcome{failure: &protocolv1.RequestFailure{SchemaVersion: 1, RequestID: metadata.ID, Code: "agent_disconnected", Message: "The provider Agent disconnected.", Retryable: true}})
	}
}

func (b *broker) online(agentID string) bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.providers[agentID] != nil
}

func (b *broker) activeRequest(requestID string) *activeRequest {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.active[requestID]
}

func (b *broker) dispatch(request protocolv1.Request, metadata protocolv1.RequestMetadata) (*activeRequest, error) {
	b.mu.Lock()
	if existing := b.active[request.ID]; existing != nil {
		b.mu.Unlock()
		return existing, nil
	}
	provider := b.providers[request.AgentID]
	if provider == nil {
		b.mu.Unlock()
		return nil, ErrAgentOffline
	}
	active := &activeRequest{metadata: metadata, started: true, done: make(chan struct{})}
	b.active[request.ID] = active
	b.mu.Unlock()
	message := protocolv1.ProviderMessage{SchemaVersion: 1, Type: protocolv1.ProviderRequest, Request: &request}
	if err := provider.send(message); err != nil {
		b.finish(active, protocolv1.StatusFailed, requestOutcome{failure: &protocolv1.RequestFailure{SchemaVersion: 1, RequestID: request.ID, Code: "agent_disconnected", Message: "The provider Agent disconnected.", Retryable: true}})
		return nil, err
	}
	return active, nil
}

// holdWrite keeps the raw write body only in process memory until a provider
// owner approves it. The body is cleared before provider execution begins and
// on every terminal path.
func (b *broker) holdWrite(request protocolv1.Request, metadata protocolv1.RequestMetadata) (*activeRequest, error) {
	b.mu.Lock()
	if existing := b.active[request.ID]; existing != nil {
		b.mu.Unlock()
		return existing, nil
	}
	if b.providers[request.AgentID] == nil {
		b.mu.Unlock()
		return nil, ErrAgentOffline
	}
	copyRequest := request
	copyRequest.Body = append([]byte(nil), request.Body...)
	active := &activeRequest{metadata: metadata, pendingRequest: &copyRequest, done: make(chan struct{})}
	if request.ExpiresAt != nil {
		active.approvalExpires = *request.ExpiresAt
	}
	b.active[request.ID] = active
	b.mu.Unlock()
	go b.expirePending(active)
	return active, nil
}

func (b *broker) expirePending(active *activeRequest) {
	pending, ok := active.pendingState()
	if !ok {
		return
	}
	delay := pending.ExpiresAt.Sub(b.now())
	if delay < 0 {
		delay = 0
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		if active.claimPendingTerminal(protocolv1.StatusExpired, b.now()) {
			b.finish(active, protocolv1.StatusExpired, requestOutcome{failure: &protocolv1.RequestFailure{SchemaVersion: 1, RequestID: pending.Metadata.ID, Code: "write_approval_expired", Message: "The write request expired before provider approval.", Retryable: false}})
		}
	case <-active.done:
	}
}

func (b *broker) pendingForOwner(memberID string) []protocolv1.PendingRequest {
	b.mu.RLock()
	var candidates []*activeRequest
	for _, active := range b.active {
		metadata := active.metadataSnapshot()
		if provider := b.providers[metadata.AgentID]; provider != nil && provider.memberID == memberID {
			candidates = append(candidates, active)
		}
	}
	b.mu.RUnlock()
	pending := make([]protocolv1.PendingRequest, 0, len(candidates))
	for _, active := range candidates {
		if item, ok := active.pendingSnapshot(b.now()); ok {
			pending = append(pending, item)
		}
	}
	return pending
}

func (b *broker) approve(requestID, baseCommit string) (*activeRequest, error) {
	active := b.activeRequest(requestID)
	if active == nil {
		return nil, ErrRequestNotPending
	}
	request, err := active.takePending(b.now())
	if err != nil {
		if errors.Is(err, ErrApprovalExpired) {
			b.finish(active, protocolv1.StatusExpired, requestOutcome{failure: &protocolv1.RequestFailure{SchemaVersion: 1, RequestID: requestID, Code: "write_approval_expired", Message: "The write request expired before provider approval.", Retryable: false}})
		}
		return nil, err
	}
	updated, err := b.store.UpdateRequestStatus(context.Background(), requestID, protocolv1.StatusRunning, b.now())
	if err != nil {
		b.finish(active, protocolv1.StatusFailed, requestOutcome{failure: &protocolv1.RequestFailure{SchemaVersion: 1, RequestID: requestID, Code: "request_state_failed", Message: "The write request state could not be updated.", Retryable: true}})
		return nil, err
	}
	active.mu.Lock()
	terminal := active.terminal
	terminalStatus := active.metadata.Status
	if !terminal {
		active.metadata = updated
	}
	active.mu.Unlock()
	if terminal {
		_, _ = b.store.UpdateRequestStatus(context.Background(), requestID, terminalStatus, b.now())
		return nil, ErrRequestNotPending
	}
	b.mu.RLock()
	provider := b.providers[request.AgentID]
	b.mu.RUnlock()
	if provider == nil {
		b.finish(active, protocolv1.StatusFailed, requestOutcome{failure: &protocolv1.RequestFailure{SchemaVersion: 1, RequestID: requestID, Code: "agent_offline", Message: "The provider Agent is offline.", Retryable: true}})
		return nil, ErrAgentOffline
	}
	message := protocolv1.ProviderMessage{SchemaVersion: 1, Type: protocolv1.ProviderRequest, BaseCommit: baseCommit, Request: &request}
	if err := message.Validate(); err != nil {
		b.finish(active, protocolv1.StatusFailed, requestOutcome{failure: &protocolv1.RequestFailure{SchemaVersion: 1, RequestID: requestID, Code: "provider_protocol_error", Message: "The approved write request is invalid.", Retryable: false}})
		return nil, err
	}
	if err := provider.send(message); err != nil {
		b.finish(active, protocolv1.StatusFailed, requestOutcome{failure: &protocolv1.RequestFailure{SchemaVersion: 1, RequestID: requestID, Code: "agent_disconnected", Message: "The provider Agent disconnected.", Retryable: true}})
		return nil, err
	}
	return active, nil
}

func (b *broker) deny(requestID string) error {
	active := b.activeRequest(requestID)
	if active == nil {
		return ErrRequestNotPending
	}
	if !active.claimPendingTerminal(protocolv1.StatusDenied, b.now()) {
		return ErrRequestNotPending
	}
	b.finish(active, protocolv1.StatusDenied, requestOutcome{failure: &protocolv1.RequestFailure{SchemaVersion: 1, RequestID: requestID, Code: "write_request_denied", Message: "The provider denied the write request.", Retryable: false}})
	return nil
}

func (b *broker) completeResponse(agentID string, response protocolv1.Response) {
	active := b.activeRequest(response.RequestID)
	if active == nil || active.metadataSnapshot().AgentID != agentID {
		return
	}
	if len(response.Answer) > protocolv1.MaxResponseBodyBytes {
		b.finish(active, protocolv1.StatusFailed, requestOutcome{failure: &protocolv1.RequestFailure{SchemaVersion: 1, RequestID: response.RequestID, Code: "response_too_large", Message: "The provider answer exceeds the v1 response limit.", Retryable: false}})
		return
	}
	if err := response.Validate(); err != nil {
		b.finish(active, protocolv1.StatusFailed, requestOutcome{failure: &protocolv1.RequestFailure{SchemaVersion: 1, RequestID: response.RequestID, Code: "provider_protocol_error", Message: "The provider returned an invalid response.", Retryable: false}})
		return
	}
	b.finish(active, protocolv1.StatusSucceeded, requestOutcome{response: &response})
}

func (b *broker) completeFailure(agentID string, failure protocolv1.RequestFailure) {
	active := b.activeRequest(failure.RequestID)
	if active == nil || active.metadataSnapshot().AgentID != agentID {
		return
	}
	if err := failure.Validate(); err != nil {
		failure = protocolv1.RequestFailure{SchemaVersion: 1, RequestID: active.metadataSnapshot().ID, Code: "provider_protocol_error", Message: "The provider returned an invalid failure.", Retryable: false}
	}
	b.finish(active, protocolv1.StatusFailed, requestOutcome{failure: &failure})
}

func (b *broker) cancel(requestID, code, message string, status protocolv1.RequestStatus) bool {
	active := b.activeRequest(requestID)
	if active == nil {
		return false
	}
	b.mu.RLock()
	metadata := active.metadataSnapshot()
	provider := b.providers[metadata.AgentID]
	b.mu.RUnlock()
	if provider != nil && active.providerStarted() {
		_ = provider.send(protocolv1.ProviderMessage{SchemaVersion: 1, Type: protocolv1.ProviderCancel, RequestID: requestID})
	}
	b.finish(active, status, requestOutcome{failure: &protocolv1.RequestFailure{SchemaVersion: 1, RequestID: requestID, Code: code, Message: message, Retryable: code == "request_timeout" || code == "agent_access_revoked"}})
	return true
}

func (b *broker) cancelMatching(agentID, requesterID string, mode protocolv1.RequestMode) {
	b.mu.RLock()
	var ids []string
	for id, active := range b.active {
		metadata := active.metadataSnapshot()
		if metadata.AgentID == agentID && metadata.RequesterID == requesterID && metadata.Mode == mode {
			ids = append(ids, id)
		}
	}
	b.mu.RUnlock()
	for _, id := range ids {
		b.cancel(id, "agent_access_revoked", "Agent access was revoked while the request was running.", protocolv1.StatusCanceled)
	}
}

func (b *broker) cancelMember(memberID string) {
	b.mu.RLock()
	var ids []string
	var sessions []*providerSession
	for id, active := range b.active {
		if active.metadataSnapshot().RequesterID == memberID {
			ids = append(ids, id)
		}
	}
	for _, session := range b.providers {
		if session.memberID == memberID {
			sessions = append(sessions, session)
		}
	}
	b.mu.RUnlock()
	for _, id := range ids {
		b.cancel(id, "requester_credential_revoked", "The requester credential was revoked.", protocolv1.StatusCanceled)
	}
	for _, session := range sessions {
		_ = session.conn.Close()
	}
}

func (b *broker) disconnectCredential(credentialID string) {
	b.mu.RLock()
	var sessions []*providerSession
	for _, session := range b.providers {
		if session.credentialID == credentialID {
			sessions = append(sessions, session)
		}
	}
	b.mu.RUnlock()
	for _, session := range sessions {
		_ = session.conn.Close()
	}
}

func (b *broker) finish(active *activeRequest, status protocolv1.RequestStatus, outcome requestOutcome) {
	active.once.Do(func() {
		metadata := active.metadataSnapshot()
		active.mu.Lock()
		active.terminal = true
		active.pendingRequest = nil
		active.metadata.Status = status
		active.metadata.UpdatedAt = b.now()
		active.mu.Unlock()
		updated, _ := b.store.UpdateRequestStatus(context.Background(), metadata.ID, status, b.now())
		active.mu.Lock()
		if updated.ID != "" {
			active.metadata = updated
		} else {
			active.metadata.Status = status
			active.metadata.UpdatedAt = b.now()
		}
		active.outcome = outcome
		active.mu.Unlock()
		b.mu.Lock()
		if b.active[metadata.ID] == active {
			delete(b.active, metadata.ID)
		}
		b.mu.Unlock()
		close(active.done)
	})
}
