// Package v1 defines PeerContext protocol v1 wire models.
package v1

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	SchemaVersion   = 1
	ProtocolVersion = "v1"

	MaxRequestBodyBytes  = 256 * 1024
	MaxResponseBodyBytes = 2 * 1024 * 1024
)

// RuntimeMode is deliberately not extensible in the MVP. A failed isolation
// check must stop service rather than select a less isolated mode.
type RuntimeMode string

const RuntimeModeIsolated RuntimeMode = "isolated_runtime"

// RequestMode is explicitly selected by the caller. It is never inferred from
// the request body.
type RequestMode string

const (
	ModeRead  RequestMode = "read"
	ModeWrite RequestMode = "write"
)

func (m RequestMode) Validate() error {
	switch m {
	case ModeRead, ModeWrite:
		return nil
	default:
		return fmt.Errorf("unsupported request mode %q", m)
	}
}

// RequestStatus describes infrastructure and approval lifecycle state, never
// the business meaning of a Codex answer.
type RequestStatus string

const (
	StatusPendingApproval RequestStatus = "pending_approval"
	StatusRunning         RequestStatus = "running"
	StatusSucceeded       RequestStatus = "succeeded"
	StatusFailed          RequestStatus = "failed"
	StatusDenied          RequestStatus = "denied"
	StatusCanceled        RequestStatus = "canceled"
	StatusExpired         RequestStatus = "expired"
)

func (s RequestStatus) Validate() error {
	switch s {
	case StatusPendingApproval, StatusRunning, StatusSucceeded,
		StatusFailed, StatusDenied, StatusCanceled, StatusExpired:
		return nil
	default:
		return fmt.Errorf("unsupported request status %q", s)
	}
}

// AgentManifest is public discovery data. It intentionally has no repository
// path or other provider-local field.
type AgentManifest struct {
	SchemaVersion int           `json:"schema_version"`
	Name          string        `json:"name"`
	Summary       string        `json:"summary"`
	Tags          []string      `json:"tags"`
	Capabilities  []string      `json:"capabilities"`
	Modes         []RequestMode `json:"modes"`
}

// Validate performs structural validation only. It does not interpret summary,
// tags, or capabilities.
func (m AgentManifest) Validate() error {
	if err := validateSchema(m.SchemaVersion); err != nil {
		return err
	}
	if strings.TrimSpace(m.Name) == "" {
		return errors.New("agent name is required")
	}
	if strings.TrimSpace(m.Summary) == "" {
		return errors.New("agent summary is required")
	}
	if len(m.Modes) == 0 {
		return errors.New("at least one request mode is required")
	}
	seen := make(map[RequestMode]struct{}, len(m.Modes))
	for _, mode := range m.Modes {
		if err := mode.Validate(); err != nil {
			return err
		}
		if _, ok := seen[mode]; ok {
			return fmt.Errorf("duplicate request mode %q", mode)
		}
		seen[mode] = struct{}{}
	}
	return nil
}

// Request is the in-memory/on-wire request. Relay persistence must use a
// separate metadata record and must not serialize Body.
type Request struct {
	SchemaVersion int         `json:"schema_version"`
	ID            string      `json:"request_id"`
	ProjectID     string      `json:"project_id"`
	RequesterID   string      `json:"requester_member_id"`
	AgentID       string      `json:"agent_id"`
	Mode          RequestMode `json:"mode"`
	Body          []byte      `json:"body"`
	BodySHA256    string      `json:"body_sha256"`
	CreatedAt     time.Time   `json:"created_at"`
	ExpiresAt     *time.Time  `json:"expires_at,omitempty"`
}

func (r Request) Validate() error {
	if err := validateSchema(r.SchemaVersion); err != nil {
		return err
	}
	for name, value := range map[string]string{
		"request_id":          r.ID,
		"project_id":          r.ProjectID,
		"requester_member_id": r.RequesterID,
		"agent_id":            r.AgentID,
	} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%s is required", name)
		}
	}
	if err := r.Mode.Validate(); err != nil {
		return err
	}
	if len(r.Body) > MaxRequestBodyBytes {
		return fmt.Errorf("request body exceeds %d bytes", MaxRequestBodyBytes)
	}
	if !validSHA256(r.BodySHA256) {
		return errors.New("body_sha256 must be a lowercase SHA-256 hex digest")
	}
	if BodySHA256(r.Body) != r.BodySHA256 {
		return errors.New("body_sha256 does not match request body")
	}
	if r.CreatedAt.IsZero() {
		return errors.New("created_at is required")
	}
	if r.ExpiresAt != nil && !r.ExpiresAt.After(r.CreatedAt) {
		return errors.New("expires_at must be after created_at")
	}
	return nil
}

// RequestMetadata is safe to persist and intentionally has no body field.
type RequestMetadata struct {
	SchemaVersion int           `json:"schema_version"`
	ID            string        `json:"request_id"`
	ProjectID     string        `json:"project_id"`
	RequesterID   string        `json:"requester_member_id"`
	AgentID       string        `json:"agent_id"`
	Mode          RequestMode   `json:"mode"`
	Status        RequestStatus `json:"status"`
	BodyBytes     int           `json:"body_bytes"`
	BodySHA256    string        `json:"body_sha256"`
	CreatedAt     time.Time     `json:"created_at"`
	UpdatedAt     time.Time     `json:"updated_at"`
}

func (m RequestMetadata) Validate() error {
	if err := validateSchema(m.SchemaVersion); err != nil {
		return err
	}
	for name, value := range map[string]string{
		"request_id":          m.ID,
		"project_id":          m.ProjectID,
		"requester_member_id": m.RequesterID,
		"agent_id":            m.AgentID,
	} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%s is required", name)
		}
	}
	if err := m.Mode.Validate(); err != nil {
		return err
	}
	if err := m.Status.Validate(); err != nil {
		return err
	}
	if m.BodyBytes < 0 || m.BodyBytes > MaxRequestBodyBytes {
		return fmt.Errorf("body_bytes must be between 0 and %d", MaxRequestBodyBytes)
	}
	if !validSHA256(m.BodySHA256) {
		return errors.New("body_sha256 must be a lowercase SHA-256 hex digest")
	}
	if m.CreatedAt.IsZero() || m.UpdatedAt.IsZero() {
		return errors.New("created_at and updated_at are required")
	}
	if m.UpdatedAt.Before(m.CreatedAt) {
		return errors.New("updated_at must not be before created_at")
	}
	return nil
}

// Metadata returns the body-free representation intended for Relay audit
// storage.
func (r Request) Metadata(status RequestStatus, updatedAt time.Time) RequestMetadata {
	return RequestMetadata{
		SchemaVersion: r.SchemaVersion,
		ID:            r.ID,
		ProjectID:     r.ProjectID,
		RequesterID:   r.RequesterID,
		AgentID:       r.AgentID,
		Mode:          r.Mode,
		Status:        status,
		BodyBytes:     len(r.Body),
		BodySHA256:    r.BodySHA256,
		CreatedAt:     r.CreatedAt,
		UpdatedAt:     updatedAt,
	}
}

// Response carries the exact final Codex message bytes. Infrastructure errors
// use the CLI error envelope instead of this answer field.
type Response struct {
	SchemaVersion int             `json:"schema_version"`
	RequestID     string          `json:"request_id"`
	Status        RequestStatus   `json:"status"`
	Answer        []byte          `json:"answer"`
	Worktree      *WorktreeResult `json:"worktree,omitempty"`
}

// WorktreeResult is safe to return through Relay. The provider-local checkout
// path and source repository path deliberately never enter the wire model.
type WorktreeResult struct {
	SchemaVersion int    `json:"schema_version"`
	ID            string `json:"worktree_id"`
	AgentID       string `json:"agent_id"`
	RequestID     string `json:"request_id"`
	BaseCommit    string `json:"base_commit"`
}

func (w WorktreeResult) Validate() error {
	if err := validateSchema(w.SchemaVersion); err != nil {
		return err
	}
	for name, value := range map[string]string{"worktree_id": w.ID, "agent_id": w.AgentID, "request_id": w.RequestID, "base_commit": w.BaseCommit} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%s is required", name)
		}
	}
	return nil
}

// RequestFailure is an infrastructure failure from the provider or Relay. It
// is intentionally separate from Response so a failed invocation can never be
// mistaken for a Codex answer.
type RequestFailure struct {
	SchemaVersion int    `json:"schema_version"`
	RequestID     string `json:"request_id"`
	Code          string `json:"code"`
	Message       string `json:"message"`
	Retryable     bool   `json:"retryable"`
}

func (f RequestFailure) Validate() error {
	if err := validateSchema(f.SchemaVersion); err != nil {
		return err
	}
	if strings.TrimSpace(f.RequestID) == "" {
		return errors.New("request_id is required")
	}
	if strings.TrimSpace(f.Code) == "" {
		return errors.New("failure code is required")
	}
	if strings.TrimSpace(f.Message) == "" {
		return errors.New("failure message is required")
	}
	return nil
}

type ProviderMessageType string

const (
	ProviderReady    ProviderMessageType = "ready"
	ProviderPing     ProviderMessageType = "ping"
	ProviderPong     ProviderMessageType = "pong"
	ProviderRequest  ProviderMessageType = "request"
	ProviderResponse ProviderMessageType = "response"
	ProviderFailure  ProviderMessageType = "failure"
	ProviderCancel   ProviderMessageType = "cancel"
)

// ProviderMessage is the only Agent serve WebSocket frame shape. Exactly one
// request/response/failure payload is present for the corresponding type.
type ProviderMessage struct {
	SchemaVersion int                 `json:"schema_version"`
	Type          ProviderMessageType `json:"type"`
	AgentID       string              `json:"agent_id,omitempty"`
	RuntimeMode   RuntimeMode         `json:"runtime_mode,omitempty"`
	RequestID     string              `json:"request_id,omitempty"`
	BaseCommit    string              `json:"base_commit,omitempty"`
	Request       *Request            `json:"request,omitempty"`
	Response      *Response           `json:"response,omitempty"`
	Failure       *RequestFailure     `json:"failure,omitempty"`
}

func (m ProviderMessage) Validate() error {
	if err := validateSchema(m.SchemaVersion); err != nil {
		return err
	}
	switch m.Type {
	case ProviderReady:
		if strings.TrimSpace(m.AgentID) == "" || m.RuntimeMode != RuntimeModeIsolated {
			return errors.New("ready message requires agent_id and isolated_runtime")
		}
	case ProviderPing, ProviderPong:
		return nil
	case ProviderRequest:
		if m.Request == nil {
			return errors.New("request message requires request")
		}
		if err := m.Request.Validate(); err != nil {
			return err
		}
		if m.Request.Mode == ModeWrite && strings.TrimSpace(m.BaseCommit) == "" {
			return errors.New("write request message requires base_commit")
		}
		if m.Request.Mode == ModeRead && m.BaseCommit != "" {
			return errors.New("read request message must not include base_commit")
		}
		return nil
	case ProviderResponse:
		if m.Response == nil {
			return errors.New("response message requires response")
		}
		return m.Response.Validate()
	case ProviderFailure:
		if m.Failure == nil {
			return errors.New("failure message requires failure")
		}
		return m.Failure.Validate()
	case ProviderCancel:
		if strings.TrimSpace(m.RequestID) == "" {
			return errors.New("cancel message requires request_id")
		}
	default:
		return fmt.Errorf("unsupported provider message type %q", m.Type)
	}
	return nil
}

// AskResult distinguishes a first delivery containing an answer from a replay
// that can only return already-persisted metadata. Relay never persists Answer.
type AskResult struct {
	SchemaVersion int              `json:"schema_version"`
	Response      *Response        `json:"response,omitempty"`
	Metadata      *RequestMetadata `json:"metadata,omitempty"`
	Replayed      bool             `json:"replayed"`
}

func (r Response) Validate() error {
	if err := validateSchema(r.SchemaVersion); err != nil {
		return err
	}
	if strings.TrimSpace(r.RequestID) == "" {
		return errors.New("request_id is required")
	}
	if err := r.Status.Validate(); err != nil {
		return err
	}
	if r.Status != StatusSucceeded {
		return errors.New("answer is only valid for a succeeded request")
	}
	if r.Answer == nil {
		return errors.New("answer is required for a succeeded request")
	}
	if len(r.Answer) > MaxResponseBodyBytes {
		return fmt.Errorf("response body exceeds %d bytes", MaxResponseBodyBytes)
	}
	if r.Worktree != nil {
		if err := r.Worktree.Validate(); err != nil {
			return err
		}
		if r.Worktree.RequestID != r.RequestID {
			return errors.New("worktree request_id does not match response")
		}
	}
	return nil
}

// PendingRequest is provider-visible approval metadata. It intentionally has
// no request body or local repository path.
type PendingRequest struct {
	SchemaVersion int             `json:"schema_version"`
	Metadata      RequestMetadata `json:"metadata"`
	ExpiresAt     time.Time       `json:"expires_at"`
}

func (p PendingRequest) Validate(now time.Time) error {
	if err := validateSchema(p.SchemaVersion); err != nil {
		return err
	}
	if err := p.Metadata.Validate(); err != nil {
		return err
	}
	if p.Metadata.Mode != ModeWrite || p.Metadata.Status != StatusPendingApproval {
		return errors.New("pending request must be a write awaiting approval")
	}
	if p.ExpiresAt.IsZero() || !p.ExpiresAt.After(now) {
		return errors.New("pending request is expired")
	}
	return nil
}

// WriteConfirmation is returned before a write request is sent. It binds the
// requester confirmation to the exact agent, explicit mode, raw-body hash, and
// expiry required by the PRD. It contains no request body.
type WriteConfirmation struct {
	SchemaVersion int         `json:"schema_version"`
	AgentID       string      `json:"agent_id"`
	Mode          RequestMode `json:"mode"`
	BodyBytes     int         `json:"body_bytes"`
	BodySHA256    string      `json:"body_sha256"`
	ExpiresAt     time.Time   `json:"expires_at"`
}

func (c WriteConfirmation) Validate(now time.Time) error {
	if err := validateSchema(c.SchemaVersion); err != nil {
		return err
	}
	if strings.TrimSpace(c.AgentID) == "" {
		return errors.New("agent_id is required")
	}
	if c.Mode != ModeWrite {
		return errors.New("write confirmation mode must be write")
	}
	if c.BodyBytes < 0 || c.BodyBytes > MaxRequestBodyBytes {
		return fmt.Errorf("body_bytes must be between 0 and %d", MaxRequestBodyBytes)
	}
	if !validSHA256(c.BodySHA256) {
		return errors.New("body_sha256 must be a lowercase SHA-256 hex digest")
	}
	if c.ExpiresAt.IsZero() || !c.ExpiresAt.After(now) {
		return errors.New("write confirmation is expired")
	}
	return nil
}

// BodySHA256 hashes raw body bytes without normalization or text decoding.
func BodySHA256(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

func validateSchema(schemaVersion int) error {
	if schemaVersion != SchemaVersion {
		return fmt.Errorf("unsupported schema_version %d", schemaVersion)
	}
	return nil
}

func validSHA256(value string) bool {
	if len(value) != sha256.Size*2 || strings.ToLower(value) != value {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size
}
