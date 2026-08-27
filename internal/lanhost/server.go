package lanhost

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	protocolv2 "github.com/wWzZb/peercontext/pkg/protocol/v2"
)

type Server struct {
	Store        *Store
	HostIdentity func(projectID string) (HostIdentity, error)
	Endpoints    func() []string
	Now          func() time.Time
	replays      *replayGuard
	broker       *broker
	upgrader     websocket.Upgrader
}

func NewServer(store *Store, identities func(string) (HostIdentity, error), endpoints func() []string) *Server {
	return &Server{
		Store: store, HostIdentity: identities, Endpoints: endpoints,
		Now: time.Now, replays: newReplayGuard(protocolv2.DefaultSignatureMaxAge), broker: newBroker(),
		upgrader: websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }},
	}
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !isDirectLANRemote(r.RemoteAddr) {
		http.Error(w, "PeerContext only accepts devices on a directly connected LAN", http.StatusForbidden)
		return
	}
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/v2/identity":
		s.handleIdentity(w, r)
	case r.Method == http.MethodPost && r.URL.Path == "/v2/join":
		s.handleJoin(w, r)
	case r.Method == http.MethodPost && r.URL.Path == "/v2/rpc":
		s.handleRPC(w, r)
	case r.Method == http.MethodGet && r.URL.Path == "/v2/provider":
		s.handleProvider(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) handleIdentity(w http.ResponseWriter, r *http.Request) {
	projectID := r.URL.Query().Get("project_id")
	challenge := r.URL.Query().Get("challenge")
	if challenge == "" {
		http.Error(w, "identity challenge is required", http.StatusBadRequest)
		return
	}
	project, err := s.Store.Project(r.Context(), projectID)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	data, _ := json.Marshal(project)
	s.writeSigned(w, projectID, "identity.response", http.MethodGet, "/v2/identity", challenge, RPCResult{OK: true, Data: data}, http.StatusOK)
}

func (s *Server) handleJoin(w http.ResponseWriter, r *http.Request) {
	var request protocolv2.JoinRequest
	if err := decodeJSON(r.Body, &request, 64*1024); err != nil {
		http.Error(w, "invalid join request", http.StatusBadRequest)
		return
	}
	if request.Method != http.MethodPost || request.Path != "/v2/join" {
		s.writeSigned(w, request.ProjectID, "join.response", http.MethodPost, "/v2/join", request.Nonce, rpcFailure("join_rejected", errors.New("join signature route mismatch"), false), http.StatusForbidden)
		return
	}
	project, err := s.Store.ConsumeInvitation(r.Context(), request, s.Now().UTC())
	if err != nil {
		s.writeSigned(w, request.ProjectID, "join.response", http.MethodPost, "/v2/join", request.Nonce, rpcFailure("join_rejected", err, false), http.StatusForbidden)
		return
	}
	member, err := s.Store.Member(r.Context(), request.ProjectID, memberIDFromPublicKey(request.MemberPublicKey))
	if err != nil {
		s.writeSigned(w, request.ProjectID, "join.response", http.MethodPost, "/v2/join", request.Nonce, rpcFailure("join_failed", err, false), http.StatusInternalServerError)
		return
	}
	data, _ := json.Marshal(JoinResult{Project: project, Member: member})
	s.writeSigned(w, request.ProjectID, "join.response", http.MethodPost, "/v2/join", request.Nonce, RPCResult{OK: true, Data: data}, http.StatusOK)
}

func (s *Server) handleRPC(w http.ResponseWriter, r *http.Request) {
	var message protocolv2.SignedMessage
	if err := decodeJSON(r.Body, &message, protocolv2.MaxWireMessageBytes); err != nil {
		http.Error(w, "invalid signed request", http.StatusBadRequest)
		return
	}
	member, err := s.authenticate(r.Context(), message, http.MethodPost, "/v2/rpc")
	if err != nil {
		s.writeSigned(w, message.ProjectID, message.Kind+".response", http.MethodPost, "/v2/rpc", message.Nonce, rpcFailure("unauthorized", err, false), http.StatusUnauthorized)
		return
	}
	result, status := s.dispatchRPC(r.Context(), member, message)
	s.writeSigned(w, message.ProjectID, message.Kind+".response", http.MethodPost, "/v2/rpc", message.Nonce, result, status)
}

func (s *Server) authenticate(ctx context.Context, message protocolv2.SignedMessage, method, path string) (protocolv2.Member, error) {
	member, err := s.Store.Member(ctx, message.ProjectID, message.SenderID)
	if err != nil {
		return member, err
	}
	if message.Method != method || message.Path != path {
		return member, errors.New("signed message route does not match HTTP request")
	}
	key, err := publicKey(member.PublicKey)
	if err != nil {
		return member, err
	}
	now := s.Now().UTC()
	if err := message.Verify(key, now, protocolv2.DefaultSignatureMaxAge); err != nil {
		return member, err
	}
	if err := s.replays.Accept(message, now); err != nil {
		return member, err
	}
	return member, nil
}

func (s *Server) dispatchRPC(ctx context.Context, member protocolv2.Member, message protocolv2.SignedMessage) (RPCResult, int) {
	switch message.Kind {
	case KindInviteCreate:
		if !member.Owner {
			return rpcFailure("forbidden", ErrMemberForbidden, false), http.StatusForbidden
		}
		var input InviteCreateInput
		if len(message.Payload) > 0 && json.Unmarshal(message.Payload, &input) != nil {
			return rpcFailure("invalid_request", errors.New("invalid invitation options"), false), http.StatusBadRequest
		}
		invitation, err := s.createInvitation(ctx, message.ProjectID, input)
		return encodeResult(invitation, err)
	case KindMembersList:
		members, err := s.Store.Members(ctx, message.ProjectID)
		return encodeResult(members, err)
	case KindMemberRemove:
		var input MemberRemoveInput
		if err := json.Unmarshal(message.Payload, &input); err != nil {
			return rpcFailure("invalid_request", err, false), http.StatusBadRequest
		}
		err := s.Store.RemoveMember(ctx, message.ProjectID, member.ID, input.MemberID)
		return encodeResult(map[string]bool{"removed": err == nil}, err)
	case KindAgentRegister:
		var input AgentRegisterInput
		if err := json.Unmarshal(message.Payload, &input); err != nil || input.AgentID == "" || input.Manifest.Validate() != nil {
			return rpcFailure("invalid_request", errors.New("invalid Agent manifest"), false), http.StatusBadRequest
		}
		agent, err := s.Store.RegisterAgent(ctx, message.ProjectID, member.ID, input.AgentID, input.Manifest, s.Now().UTC())
		return encodeResult(agent, err)
	case KindAgentsList:
		agents, err := s.Store.Agents(ctx, message.ProjectID)
		return encodeResult(agents, err)
	case KindAgentGet:
		var input AgentSelectorInput
		if err := json.Unmarshal(message.Payload, &input); err != nil {
			return rpcFailure("invalid_request", err, false), http.StatusBadRequest
		}
		agent, err := s.Store.Agent(ctx, message.ProjectID, input.Agent)
		return encodeResult(agent, err)
	case KindAgentRemove:
		var input AgentSelectorInput
		if err := json.Unmarshal(message.Payload, &input); err != nil {
			return rpcFailure("invalid_request", err, false), http.StatusBadRequest
		}
		err := s.Store.RemoveAgent(ctx, message.ProjectID, member.ID, input.Agent)
		return encodeResult(map[string]bool{"removed": err == nil}, err)
	case KindRequestAsk:
		return s.ask(ctx, member, message)
	default:
		return rpcFailure("unknown_operation", fmt.Errorf("unknown operation %q", message.Kind), false), http.StatusNotFound
	}
}

func (s *Server) createInvitation(ctx context.Context, projectID string, input InviteCreateInput) (protocolv2.Invitation, error) {
	project, err := s.Store.Project(ctx, projectID)
	if err != nil {
		return protocolv2.Invitation{}, err
	}
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return protocolv2.Invitation{}, err
	}
	ttl := protocolv2.DefaultInviteTTL
	if input.TTLSeconds > 0 && time.Duration(input.TTLSeconds)*time.Second <= time.Hour {
		ttl = time.Duration(input.TTLSeconds) * time.Second
	}
	invitation := protocolv2.Invitation{
		SchemaVersion: protocolv2.SchemaVersion, ProtocolVersion: protocolv2.ProtocolVersion,
		ProjectID: project.ID, ProjectName: project.Name, Endpoints: s.Endpoints(), HostPublicKey: project.HostPublicKey,
		InviteID: "inv_" + uuid.NewString(), InvitePrivateKey: privateKey, ExpiresAt: s.Now().UTC().Add(ttl),
	}
	if err := s.Store.AddInvitation(ctx, projectID, invitation.InviteID, privateKey.Public().(ed25519.PublicKey), invitation.ExpiresAt); err != nil {
		return protocolv2.Invitation{}, err
	}
	return invitation, nil
}

func (s *Server) ask(ctx context.Context, member protocolv2.Member, message protocolv2.SignedMessage) (RPCResult, int) {
	var request protocolv2.Request
	if err := json.Unmarshal(message.Payload, &request); err != nil || request.Validate() != nil || request.ProjectID != message.ProjectID || request.RequesterID != member.ID {
		return rpcFailure("invalid_request", errors.New("invalid read request"), false), http.StatusBadRequest
	}
	agent, err := s.Store.Agent(ctx, message.ProjectID, request.AgentID)
	if err != nil {
		return rpcFailure("agent_not_found", err, false), http.StatusNotFound
	}
	if !agent.Online {
		return rpcFailure("agent_offline", ErrAgentOffline, true), http.StatusServiceUnavailable
	}
	if err := s.Store.CreateRequest(ctx, request); err != nil {
		return rpcFailure("request_failed", err, false), http.StatusConflict
	}
	identity, err := s.HostIdentity(message.ProjectID)
	if err != nil {
		return rpcFailure("host_identity_unavailable", err, true), http.StatusServiceUnavailable
	}
	response, err := s.broker.dispatch(ctx, request, identity)
	if err != nil {
		_ = s.Store.UpdateRequest(context.Background(), request.ProjectID, request.ID, protocolv2.StatusFailed, s.Now().UTC())
		return rpcFailure("agent_unavailable", err, true), http.StatusServiceUnavailable
	}
	status := protocolv2.StatusSucceeded
	if response.Type == protocolv2.ProviderFailure {
		status = protocolv2.StatusFailed
	}
	_ = s.Store.UpdateRequest(context.Background(), request.ProjectID, request.ID, status, s.Now().UTC())
	return encodeResult(response, nil)
}

func (s *Server) handleProvider(w http.ResponseWriter, r *http.Request) {
	encoded := r.Header.Get("X-Peerctx-Auth")
	data, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		http.Error(w, "provider authentication is missing", http.StatusUnauthorized)
		return
	}
	var message protocolv2.SignedMessage
	if err = json.Unmarshal(data, &message); err != nil || message.Kind != KindProviderConnect {
		http.Error(w, "provider authentication is invalid", http.StatusUnauthorized)
		return
	}
	member, err := s.authenticate(r.Context(), message, http.MethodGet, "/v2/provider")
	if err != nil {
		http.Error(w, "provider authentication failed", http.StatusUnauthorized)
		return
	}
	var input ProviderConnectInput
	if err := json.Unmarshal(message.Payload, &input); err != nil {
		http.Error(w, "provider request is invalid", http.StatusBadRequest)
		return
	}
	agent, err := s.Store.Agent(r.Context(), message.ProjectID, input.AgentID)
	if err != nil || agent.OwnerMemberID != member.ID {
		http.Error(w, "provider does not own this Agent", http.StatusForbidden)
		return
	}
	identity, err := s.HostIdentity(message.ProjectID)
	if err != nil {
		http.Error(w, "host identity unavailable", http.StatusServiceUnavailable)
		return
	}
	connectedPayload, _ := json.Marshal(ProviderConnectInput{AgentID: agent.ID})
	connectedNonce, err := newNonce()
	if err != nil {
		http.Error(w, "host signing failed", http.StatusInternalServerError)
		return
	}
	connected := protocolv2.NewSignedReply(message.ProjectID, identity.MemberID, "provider.connected", http.MethodGet, "/v2/provider", message.Nonce, connectedNonce, connectedPayload, s.Now().UTC(), ed25519.PrivateKey(identity.PrivateKey))
	connectedJSON, _ := json.Marshal(connected)
	responseHeader := http.Header{"X-Peerctx-Host-Auth": []string{base64.RawURLEncoding.EncodeToString(connectedJSON)}}
	conn, err := s.upgrader.Upgrade(w, r, responseHeader)
	if err != nil {
		return
	}
	provider := &providerConnection{projectID: message.ProjectID, agentID: agent.ID, memberID: member.ID, key: member.PublicKey, conn: conn}
	conn.SetReadLimit(protocolv2.MaxWireMessageBytes)
	if old := s.broker.attach(provider); old != nil {
		_ = old.conn.Close()
	}
	_ = s.Store.SetAgentOnline(context.Background(), provider.projectID, provider.agentID, provider.memberID, true, s.Now().UTC())
	defer func() {
		s.broker.detach(provider)
		_ = s.Store.SetAgentOnline(context.Background(), provider.projectID, provider.agentID, provider.memberID, false, s.Now().UTC())
		_ = conn.Close()
	}()
	for {
		var incoming protocolv2.SignedMessage
		if err := conn.ReadJSON(&incoming); err != nil {
			return
		}
		if err := s.broker.receive(incoming, provider); err != nil {
			return
		}
	}
}

func (s *Server) writeSigned(w http.ResponseWriter, projectID, kind, method, path, replyTo string, result RPCResult, status int) {
	identity, err := s.HostIdentity(projectID)
	if err != nil || len(identity.PrivateKey) != ed25519.PrivateKeySize {
		http.Error(w, "host identity unavailable", http.StatusServiceUnavailable)
		return
	}
	payload, _ := json.Marshal(result)
	nonce, err := newNonce()
	if err != nil {
		http.Error(w, "host signing failed", http.StatusInternalServerError)
		return
	}
	message := protocolv2.NewSignedReply(projectID, identity.MemberID, kind, method, path, replyTo, nonce, payload, s.Now().UTC(), ed25519.PrivateKey(identity.PrivateKey))
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(message)
}

func encodeResult(value any, err error) (RPCResult, int) {
	if err != nil {
		return rpcFailure("operation_failed", err, false), http.StatusBadRequest
	}
	data, marshalErr := json.Marshal(value)
	if marshalErr != nil {
		return rpcFailure("encode_failed", marshalErr, false), http.StatusInternalServerError
	}
	return RPCResult{OK: true, Data: data}, http.StatusOK
}

func rpcFailure(code string, err error, retryable bool) RPCResult {
	return RPCResult{OK: false, Error: &RPCError{Code: code, Message: err.Error(), Retryable: retryable}}
}

func decodeJSON(reader io.Reader, target any, limit int64) error {
	decoder := json.NewDecoder(io.LimitReader(reader, limit))
	decoder.DisallowUnknownFields()
	return decoder.Decode(target)
}

func normalizeEndpoint(endpoint string) string { return strings.TrimRight(endpoint, "/") }
