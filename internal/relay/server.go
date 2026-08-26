package relay

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	protocolv1 "github.com/wWzZb/peercontext/pkg/protocol/v1"
)

const (
	maxControlBodyBytes   = 1 << 20
	maxProviderFrameBytes = protocolv1.MaxResponseBodyBytes*4/3 + 64*1024
)

type Server struct {
	store        *Store
	logger       *slog.Logger
	broker       *broker
	readTimeout  time.Duration
	writeTimeout time.Duration
	upgrader     websocket.Upgrader
}

type ServerOption func(*Server)

func WithReadTimeout(timeout time.Duration) ServerOption {
	return func(server *Server) {
		if timeout > 0 {
			server.readTimeout = timeout
		}
	}
}

func WithWriteTimeout(timeout time.Duration) ServerOption {
	return func(server *Server) {
		if timeout > 0 {
			server.writeTimeout = timeout
		}
	}
}

func NewServer(store *Store, logger *slog.Logger, options ...ServerOption) (*Server, error) {
	if store == nil {
		return nil, errors.New("relay store is required")
	}
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(discardWriter{}, nil))
	}
	server := &Server{store: store, logger: logger, broker: newBroker(store), readTimeout: 5 * time.Minute, writeTimeout: 15 * time.Minute, upgrader: websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}}
	for _, option := range options {
		option(server)
	}
	return server, nil
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.health)
	mux.HandleFunc("POST /v1/projects", s.createProject)
	mux.HandleFunc("POST /v1/project/join", s.joinProject)
	mux.HandleFunc("POST /v1/project/invites", s.createInvite)
	mux.HandleFunc("GET /v1/project/members", s.listMembers)
	mux.HandleFunc("POST /v1/project/members/{member}/promote", s.promoteMember)
	mux.HandleFunc("DELETE /v1/project/members/{member}", s.removeMember)
	mux.HandleFunc("GET /v1/credential/status", s.credentialStatus)
	mux.HandleFunc("POST /v1/credential/rotate", s.rotateCredential)
	mux.HandleFunc("DELETE /v1/credential", s.revokeCredential)
	mux.HandleFunc("DELETE /v1/credentials/{credential}", s.revokeCredential)
	mux.HandleFunc("POST /v1/agents", s.registerAgent)
	mux.HandleFunc("GET /v1/agents", s.listAgents)
	mux.HandleFunc("GET /v1/agents/{agent}", s.getAgent)
	mux.HandleFunc("POST /v1/agents/{agent}/access", s.setAgentAccess)
	mux.HandleFunc("GET /v1/agents/{agent}/serve", s.serveAgent)
	mux.HandleFunc("POST /v1/requests", s.createRequest)
	mux.HandleFunc("GET /v1/requests/pending", s.listPendingRequests)
	mux.HandleFunc("GET /v1/requests/{request}", s.getRequest)
	mux.HandleFunc("DELETE /v1/requests/{request}", s.cancelRequest)
	mux.HandleFunc("POST /v1/requests/{request}/approve", s.approveRequest)
	mux.HandleFunc("POST /v1/requests/{request}/deny", s.denyRequest)
	return s.safeLog(mux)
}

func (s *Server) safeLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		wrapped := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(wrapped, r)
		// Never log headers, URL values, bodies, responses, or local paths.
		s.logger.Info("relay request",
			"method", r.Method,
			"status", wrapped.status,
			"duration_ms", time.Since(started).Milliseconds(),
		)
	})
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"schema_version": 1, "status": "ok"})
}

type createProjectRequest struct {
	Name      string `json:"name"`
	OwnerName string `json:"owner_name"`
}

func (s *Server) createProject(w http.ResponseWriter, r *http.Request) {
	var input createProjectRequest
	if !decodeJSON(w, r, &input) {
		return
	}
	project, member, token, err := s.store.CreateProject(r.Context(), input.Name, input.OwnerName)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"schema_version": 1, "project": project, "member": member, "credential_token": token})
}

type joinProjectRequest struct {
	InviteToken string `json:"invite_token"`
	MemberName  string `json:"member_name"`
}

func (s *Server) joinProject(w http.ResponseWriter, r *http.Request) {
	var input joinProjectRequest
	if !decodeJSON(w, r, &input) {
		return
	}
	project, member, token, err := s.store.JoinInvite(r.Context(), input.InviteToken, input.MemberName)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"schema_version": 1, "project": project, "member": member, "credential_token": token})
}

type createInviteRequest struct {
	ExpiresInSeconds int64 `json:"expires_in_seconds"`
}

func (s *Server) createInvite(w http.ResponseWriter, r *http.Request) {
	principal, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	var input createInviteRequest
	if !decodeJSON(w, r, &input) {
		return
	}
	if input.ExpiresInSeconds == 0 {
		input.ExpiresInSeconds = 600
	}
	invite, token, err := s.store.CreateInvite(r.Context(), principal, time.Duration(input.ExpiresInSeconds)*time.Second)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"schema_version": 1, "invite": invite, "invite_token": token})
}

func (s *Server) listMembers(w http.ResponseWriter, r *http.Request) {
	principal, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	members, err := s.store.ListMembers(r.Context(), principal)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"schema_version": 1, "members": members})
}
func (s *Server) promoteMember(w http.ResponseWriter, r *http.Request) {
	principal, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	member, err := s.store.PromoteMember(r.Context(), principal, r.PathValue("member"))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, member)
}
func (s *Server) removeMember(w http.ResponseWriter, r *http.Request) {
	principal, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	if err := s.store.RemoveMember(r.Context(), principal, r.PathValue("member")); err != nil {
		writeStoreError(w, err)
		return
	}
	s.broker.cancelMember(r.PathValue("member"))
	writeJSON(w, http.StatusOK, map[string]any{"schema_version": 1, "removed": true})
}
func (s *Server) credentialStatus(w http.ResponseWriter, r *http.Request) {
	principal, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"schema_version": 1, "project": principal.Project, "member": principal.Member, "credential": principal.Credential})
}
func (s *Server) rotateCredential(w http.ResponseWriter, r *http.Request) {
	principal, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	credential, token, err := s.store.RotateCredential(r.Context(), principal)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	s.broker.disconnectCredential(principal.Credential.ID)
	writeJSON(w, http.StatusOK, map[string]any{"schema_version": 1, "credential": credential, "credential_token": token})
}
func (s *Server) revokeCredential(w http.ResponseWriter, r *http.Request) {
	principal, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	credentialID := r.PathValue("credential")
	if credentialID == "" {
		credentialID = principal.Credential.ID
	}
	memberID, err := s.store.CredentialMemberID(r.Context(), principal.Project.ID, credentialID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if err := s.store.RevokeCredential(r.Context(), principal, credentialID); err != nil {
		writeStoreError(w, err)
		return
	}
	s.broker.disconnectCredential(credentialID)
	s.broker.cancelMember(memberID)
	writeJSON(w, http.StatusOK, map[string]any{"schema_version": 1, "revoked": true})
}

func (s *Server) registerAgent(w http.ResponseWriter, r *http.Request) {
	principal, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	var manifest protocolv1.AgentManifest
	if !decodeJSON(w, r, &manifest) {
		return
	}
	agent, err := s.store.RegisterAgent(r.Context(), principal, manifest)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	agent.Online = s.broker.online(agent.ID)
	writeJSON(w, http.StatusCreated, agent)
}
func (s *Server) listAgents(w http.ResponseWriter, r *http.Request) {
	principal, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	agents, err := s.store.ListAgents(r.Context(), principal)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	for i := range agents {
		agents[i].Online = s.broker.online(agents[i].ID)
	}
	writeJSON(w, http.StatusOK, map[string]any{"schema_version": 1, "agents": agents})
}
func (s *Server) getAgent(w http.ResponseWriter, r *http.Request) {
	principal, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	agent, err := s.store.GetAgent(r.Context(), principal, r.PathValue("agent"))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	agent.Online = s.broker.online(agent.ID)
	writeJSON(w, http.StatusOK, agent)
}

type accessRequest struct {
	MemberID string                   `json:"member_id"`
	Modes    []protocolv1.RequestMode `json:"modes"`
	Grant    bool                     `json:"grant"`
}

func (s *Server) setAgentAccess(w http.ResponseWriter, r *http.Request) {
	principal, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	var input accessRequest
	if !decodeJSON(w, r, &input) {
		return
	}
	acl, err := s.store.SetACL(r.Context(), principal, r.PathValue("agent"), input.MemberID, input.Modes, input.Grant)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if !input.Grant {
		for _, mode := range input.Modes {
			s.broker.cancelMatching(acl.AgentID, input.MemberID, mode)
		}
	}
	writeJSON(w, http.StatusOK, acl)
}

func (s *Server) serveAgent(w http.ResponseWriter, r *http.Request) {
	principal, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	agent, err := s.store.GetAgent(r.Context(), principal, r.PathValue("agent"))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if agent.OwnerMemberID != principal.Member.ID {
		writeStoreError(w, ErrForbidden)
		return
	}
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	session, err := s.broker.registerProvider(agent.ID, principal, conn)
	if err != nil {
		_ = conn.WriteJSON(protocolv1.ProviderMessage{SchemaVersion: 1, Type: protocolv1.ProviderFailure, Failure: &protocolv1.RequestFailure{SchemaVersion: 1, RequestID: "connection", Code: "agent_already_serving", Message: "This Agent already has an active provider connection."}})
		_ = conn.Close()
		return
	}
	defer func() { s.broker.unregisterProvider(session); _ = conn.Close() }()
	_ = session.send(protocolv1.ProviderMessage{SchemaVersion: 1, Type: protocolv1.ProviderReady, AgentID: agent.ID, RuntimeMode: protocolv1.RuntimeModeIsolated})
	conn.SetReadLimit(maxProviderFrameBytes)
	for {
		var message protocolv1.ProviderMessage
		if err := conn.ReadJSON(&message); err != nil {
			return
		}
		switch message.Type {
		case protocolv1.ProviderPing:
			if err := session.send(protocolv1.ProviderMessage{SchemaVersion: 1, Type: protocolv1.ProviderPong}); err != nil {
				return
			}
		case protocolv1.ProviderResponse:
			if message.Response != nil {
				s.broker.completeResponse(agent.ID, *message.Response)
			}
		case protocolv1.ProviderFailure:
			if message.Failure != nil {
				s.broker.completeFailure(agent.ID, *message.Failure)
			}
		}
	}
}

func (s *Server) createRequest(w http.ResponseWriter, r *http.Request) {
	principal, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	var request protocolv1.Request
	if !decodeJSON(w, r, &request) {
		return
	}
	if err := request.Validate(); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_request", "The v1 request is invalid.")
		return
	}
	if request.ProjectID != principal.Project.ID || request.RequesterID != principal.Member.ID {
		writeAPIError(w, http.StatusForbidden, "request_identity_mismatch", "The request identity does not match the credential.")
		return
	}
	if request.Mode == protocolv1.ModeWrite && request.ExpiresAt == nil {
		writeAPIError(w, http.StatusBadRequest, "write_approval_expiry_required", "A write request requires an approval expiry.")
		return
	}
	if request.ExpiresAt != nil && !request.ExpiresAt.After(time.Now().UTC()) {
		writeAPIError(w, http.StatusRequestTimeout, "request_expired", "The request has expired.")
		return
	}
	agent, err := s.store.GetAgent(r.Context(), principal, request.AgentID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	allowed, err := s.store.HasAccess(r.Context(), principal, agent.ID, request.Mode)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if !allowed {
		writeAPIError(w, http.StatusForbidden, "agent_access_denied", "The member cannot use this Agent in the requested mode.")
		return
	}
	online := s.broker.online(agent.ID)
	initialStatus := protocolv1.StatusRunning
	if request.Mode == protocolv1.ModeWrite {
		initialStatus = protocolv1.StatusPendingApproval
	}
	if !online {
		initialStatus = protocolv1.StatusFailed
	}
	metadata, replayed, err := s.store.BeginRequest(r.Context(), request, initialStatus, time.Now().UTC())
	if errors.Is(err, ErrReplayMismatch) {
		writeAPIError(w, http.StatusConflict, "request_replay_mismatch", "The request ID is already bound to different request bytes or identities.")
		return
	}
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if replayed {
		if active := s.broker.activeRequest(request.ID); active != nil {
			s.waitForRequest(w, r, active, true, s.requestWaitTimeout(request))
			return
		}
		writeJSON(w, http.StatusOK, protocolv1.AskResult{SchemaVersion: 1, Metadata: &metadata, Replayed: true})
		return
	}
	if !online {
		writeDetailedAPIError(w, http.StatusServiceUnavailable, "agent_offline", "The provider Agent is offline.", true)
		return
	}
	var active *activeRequest
	if request.Mode == protocolv1.ModeWrite {
		active, err = s.broker.holdWrite(request, metadata)
	} else {
		active, err = s.broker.dispatch(request, metadata)
	}
	if err != nil {
		_, _ = s.store.UpdateRequestStatus(r.Context(), request.ID, protocolv1.StatusFailed, time.Now().UTC())
		writeDetailedAPIError(w, http.StatusServiceUnavailable, "agent_offline", "The provider Agent is offline.", true)
		return
	}
	allowed, err = s.store.HasAccess(r.Context(), principal, agent.ID, request.Mode)
	if err != nil || !allowed {
		s.broker.cancel(request.ID, "agent_access_revoked", "Agent access was revoked before execution could continue.", protocolv1.StatusCanceled)
	}
	s.waitForRequest(w, r, active, false, s.requestWaitTimeout(request))
}

func (s *Server) requestWaitTimeout(request protocolv1.Request) time.Duration {
	if request.Mode == protocolv1.ModeWrite {
		remaining := time.Until(*request.ExpiresAt)
		if remaining < 0 {
			remaining = 0
		}
		return remaining + s.writeTimeout
	}
	return requestTimeout(s.readTimeout, request.ExpiresAt)
}

func (s *Server) waitForRequest(w http.ResponseWriter, r *http.Request, active *activeRequest, replayed bool, timeout time.Duration) {
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()
	outcome, err := active.wait(ctx)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			metadata := active.metadataSnapshot()
			s.broker.cancel(metadata.ID, "request_timeout", "The request timed out.", protocolv1.StatusExpired)
			writeDetailedAPIError(w, http.StatusRequestTimeout, "request_timeout", "The request timed out.", true)
		} else {
			metadata := active.metadataSnapshot()
			s.broker.cancel(metadata.ID, "request_canceled", "The requester canceled the request.", protocolv1.StatusCanceled)
		}
		return
	}
	if outcome.failure != nil {
		status := http.StatusBadGateway
		if outcome.failure.Code == "request_timeout" {
			status = http.StatusRequestTimeout
		}
		if outcome.failure.Code == "request_canceled" {
			status = http.StatusConflict
		}
		if outcome.failure.Code == "agent_access_revoked" {
			status = http.StatusForbidden
		}
		if outcome.failure.Code == "write_request_denied" {
			status = http.StatusConflict
		}
		if outcome.failure.Code == "write_approval_expired" {
			status = http.StatusRequestTimeout
		}
		writeDetailedAPIError(w, status, outcome.failure.Code, outcome.failure.Message, outcome.failure.Retryable)
		return
	}
	writeJSON(w, http.StatusOK, protocolv1.AskResult{SchemaVersion: 1, Response: outcome.response, Replayed: replayed})
}

func (s *Server) listPendingRequests(w http.ResponseWriter, r *http.Request) {
	principal, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"schema_version": 1, "requests": s.broker.pendingForOwner(principal.Member.ID)})
}

type approveRequestInput struct {
	BaseCommit string `json:"base_commit"`
}

func (s *Server) approveRequest(w http.ResponseWriter, r *http.Request) {
	principal, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	requestID := r.PathValue("request")
	metadata, agent, ok := s.authorizeProviderRequest(w, r, principal, requestID)
	if !ok {
		return
	}
	_ = agent
	var input approveRequestInput
	if !decodeJSON(w, r, &input) {
		return
	}
	input.BaseCommit = strings.TrimSpace(input.BaseCommit)
	if input.BaseCommit == "" || len(input.BaseCommit) > 256 || strings.ContainsAny(input.BaseCommit, "\r\n\x00") {
		writeAPIError(w, http.StatusBadRequest, "base_commit_required", "A single explicit base commit is required.")
		return
	}
	active, err := s.broker.approve(requestID, input.BaseCommit)
	if err != nil {
		switch {
		case errors.Is(err, ErrApprovalExpired):
			writeAPIError(w, http.StatusRequestTimeout, "write_approval_expired", "The write request expired before provider approval.")
		case errors.Is(err, ErrRequestNotPending):
			writeAPIError(w, http.StatusConflict, "request_not_pending", "The write request is no longer pending approval.")
		case errors.Is(err, ErrAgentOffline):
			writeDetailedAPIError(w, http.StatusServiceUnavailable, "agent_offline", "The provider Agent is offline.", true)
		default:
			writeAPIError(w, http.StatusBadGateway, "approval_dispatch_failed", "The approved write request could not be dispatched.")
		}
		return
	}
	metadata = active.metadataSnapshot()
	writeJSON(w, http.StatusOK, metadata)
}

func (s *Server) denyRequest(w http.ResponseWriter, r *http.Request) {
	principal, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	requestID := r.PathValue("request")
	metadata, _, ok := s.authorizeProviderRequest(w, r, principal, requestID)
	if !ok {
		return
	}
	if err := s.broker.deny(requestID); err != nil {
		writeAPIError(w, http.StatusConflict, "request_not_pending", "The write request is no longer pending approval.")
		return
	}
	metadata, _ = s.store.GetRequestMetadata(r.Context(), metadata.ID)
	writeJSON(w, http.StatusOK, metadata)
}

func (s *Server) authorizeProviderRequest(w http.ResponseWriter, r *http.Request, principal Principal, requestID string) (protocolv1.RequestMetadata, Agent, bool) {
	metadata, err := s.store.GetRequestMetadata(r.Context(), requestID)
	if err != nil {
		writeStoreError(w, err)
		return protocolv1.RequestMetadata{}, Agent{}, false
	}
	agent, err := s.store.GetAgent(r.Context(), principal, metadata.AgentID)
	if err != nil {
		writeStoreError(w, err)
		return protocolv1.RequestMetadata{}, Agent{}, false
	}
	if metadata.ProjectID != principal.Project.ID || agent.OwnerMemberID != principal.Member.ID || metadata.Mode != protocolv1.ModeWrite {
		writeStoreError(w, ErrForbidden)
		return protocolv1.RequestMetadata{}, Agent{}, false
	}
	return metadata, agent, true
}

func requestTimeout(maximum time.Duration, expiresAt *time.Time) time.Duration {
	if expiresAt == nil {
		return maximum
	}
	remaining := time.Until(*expiresAt)
	if remaining <= 0 {
		return time.Nanosecond
	}
	if remaining < maximum {
		return remaining
	}
	return maximum
}

func (s *Server) getRequest(w http.ResponseWriter, r *http.Request) {
	principal, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	metadata, err := s.store.GetRequestMetadata(r.Context(), r.PathValue("request"))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if metadata.ProjectID != principal.Project.ID || metadata.RequesterID != principal.Member.ID {
		agent, agentErr := s.store.GetAgent(r.Context(), principal, metadata.AgentID)
		if agentErr != nil || agent.OwnerMemberID != principal.Member.ID {
			writeStoreError(w, ErrForbidden)
			return
		}
	}
	writeJSON(w, http.StatusOK, metadata)
}

func (s *Server) cancelRequest(w http.ResponseWriter, r *http.Request) {
	principal, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	metadata, err := s.store.GetRequestMetadata(r.Context(), r.PathValue("request"))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if metadata.ProjectID != principal.Project.ID || metadata.RequesterID != principal.Member.ID {
		writeStoreError(w, ErrForbidden)
		return
	}
	s.broker.cancel(metadata.ID, "request_canceled", "The requester canceled the request.", protocolv1.StatusCanceled)
	updated, _ := s.store.GetRequestMetadata(r.Context(), metadata.ID)
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) authenticate(w http.ResponseWriter, r *http.Request) (Principal, bool) {
	header := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if !strings.HasPrefix(header, prefix) {
		writeAPIError(w, http.StatusUnauthorized, "authentication_required", "A valid project credential is required.")
		return Principal{}, false
	}
	principal, err := s.store.Authenticate(r.Context(), strings.TrimSpace(strings.TrimPrefix(header, prefix)))
	if err != nil {
		writeStoreError(w, err)
		return Principal{}, false
	}
	return principal, true
}

func decodeJSON(w http.ResponseWriter, r *http.Request, target any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxControlBodyBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_json", "The request JSON is invalid.")
		return false
	}
	return true
}
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func writeAPIError(w http.ResponseWriter, status int, code, message string) {
	writeDetailedAPIError(w, status, code, message, false)
}
func writeDetailedAPIError(w http.ResponseWriter, status int, code, message string, retryable bool) {
	writeJSON(w, status, map[string]any{"ok": false, "error": map[string]any{"code": code, "message": message, "retryable": retryable}})
}
func writeStoreError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrUnauthenticated):
		writeAPIError(w, http.StatusUnauthorized, "invalid_credential", "The project credential is invalid or revoked.")
	case errors.Is(err, ErrForbidden):
		writeAPIError(w, http.StatusForbidden, "forbidden", "The member is not allowed to perform this operation.")
	case errors.Is(err, ErrNotFound):
		writeAPIError(w, http.StatusNotFound, "not_found", "The requested object does not exist.")
	case errors.Is(err, ErrConflict):
		writeAPIError(w, http.StatusConflict, "conflict", "The object conflicts with existing metadata.")
	case errors.Is(err, ErrInviteConsumed):
		writeAPIError(w, http.StatusConflict, "invite_consumed", "The one-time invite has already been consumed.")
	case errors.Is(err, ErrInviteExpired):
		writeAPIError(w, http.StatusGone, "invite_expired", "The invite has expired.")
	case errors.Is(err, ErrLastOwner):
		writeAPIError(w, http.StatusConflict, "last_owner", "The final project owner cannot be removed.")
	default:
		writeAPIError(w, http.StatusBadRequest, "invalid_request", err.Error())
	}
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *statusWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker, ok := w.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, errors.New("response writer does not support hijacking")
	}
	return hijacker.Hijack()
}

func (w *statusWriter) Flush() {
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

// ValidateTLSRequirement enforces TLS whenever Relay listens beyond loopback.
func ValidateTLSRequirement(address, certFile, keyFile string) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("invalid listen address: %w", err)
	}
	if host == "" {
		host = "0.0.0.0"
	}
	ip := net.ParseIP(host)
	loopback := host == "localhost" || (ip != nil && ip.IsLoopback())
	if !loopback && (certFile == "" || keyFile == "") {
		return errors.New("TLS certificate and key are required for non-loopback Relay listeners")
	}
	if (certFile == "") != (keyFile == "") {
		return errors.New("TLS certificate and key must be provided together")
	}
	return nil
}

func Serve(ctx context.Context, address, certFile, keyFile string, handler http.Handler, ready func(net.Addr) error) error {
	if err := ValidateTLSRequirement(address, certFile, keyFile); err != nil {
		return err
	}
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return err
	}
	defer listener.Close()
	if ready != nil {
		if err := ready(listener.Addr()); err != nil {
			return err
		}
	}
	server := &http.Server{Addr: address, Handler: handler, ReadHeaderTimeout: 10 * time.Second}
	errCh := make(chan error, 1)
	go func() {
		if certFile != "" {
			errCh <- server.ServeTLS(listener, certFile, keyFile)
		} else {
			errCh <- server.Serve(listener)
		}
	}()
	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return server.Shutdown(shutdownCtx)
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}
