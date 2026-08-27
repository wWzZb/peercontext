// Package v2 defines the LAN-first PeerContext wire protocol.
package v2

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	SchemaVersion          = 2
	ProtocolVersion        = "v2"
	InvitationPrefix       = "peerctx2_"
	MaxRequestBodyBytes    = 256 * 1024
	MaxResponseBodyBytes   = 2 * 1024 * 1024
	MaxWireMessageBytes    = 8 * 1024 * 1024
	DefaultInviteTTL       = 10 * time.Minute
	DefaultRequestTimeout  = 5 * time.Minute
	DefaultSignatureMaxAge = 2 * time.Minute
)

type Project struct {
	SchemaVersion int       `json:"schema_version"`
	ID            string    `json:"project_id"`
	Name          string    `json:"name"`
	HostPublicKey []byte    `json:"host_public_key"`
	CreatedAt     time.Time `json:"created_at"`
}

type Member struct {
	SchemaVersion int       `json:"schema_version"`
	ID            string    `json:"member_id"`
	ProjectID     string    `json:"project_id"`
	Name          string    `json:"name"`
	PublicKey     []byte    `json:"public_key"`
	Owner         bool      `json:"owner"`
	CreatedAt     time.Time `json:"created_at"`
}

type AgentManifest struct {
	SchemaVersion int      `json:"schema_version"`
	Name          string   `json:"name"`
	Summary       string   `json:"summary,omitempty"`
	Tags          []string `json:"tags,omitempty"`
	Capabilities  []string `json:"capabilities,omitempty"`
}

func (m AgentManifest) Validate() error {
	if m.SchemaVersion != SchemaVersion {
		return fmt.Errorf("unsupported schema_version %d", m.SchemaVersion)
	}
	if strings.TrimSpace(m.Name) == "" {
		return errors.New("agent name is required")
	}
	return nil
}

type Agent struct {
	SchemaVersion int           `json:"schema_version"`
	ID            string        `json:"agent_id"`
	ProjectID     string        `json:"project_id"`
	OwnerMemberID string        `json:"owner_member_id"`
	Manifest      AgentManifest `json:"manifest"`
	Online        bool          `json:"online"`
	UpdatedAt     time.Time     `json:"updated_at"`
}

type RequestStatus string

const (
	StatusRunning   RequestStatus = "running"
	StatusSucceeded RequestStatus = "succeeded"
	StatusFailed    RequestStatus = "failed"
	StatusCanceled  RequestStatus = "canceled"
	StatusExpired   RequestStatus = "expired"
)

type Request struct {
	SchemaVersion int       `json:"schema_version"`
	ID            string    `json:"request_id"`
	ProjectID     string    `json:"project_id"`
	RequesterID   string    `json:"requester_member_id"`
	AgentID       string    `json:"agent_id"`
	Body          []byte    `json:"body"`
	BodySHA256    string    `json:"body_sha256"`
	CreatedAt     time.Time `json:"created_at"`
}

func (r Request) Validate() error {
	if r.SchemaVersion != SchemaVersion {
		return fmt.Errorf("unsupported schema_version %d", r.SchemaVersion)
	}
	if r.ID == "" || r.ProjectID == "" || r.RequesterID == "" || r.AgentID == "" {
		return errors.New("request identity fields are required")
	}
	if len(r.Body) > MaxRequestBodyBytes {
		return fmt.Errorf("request body exceeds %d bytes", MaxRequestBodyBytes)
	}
	if BodySHA256(r.Body) != r.BodySHA256 {
		return errors.New("body_sha256 does not match request body")
	}
	if r.CreatedAt.IsZero() {
		return errors.New("created_at is required")
	}
	return nil
}

type RequestMetadata struct {
	SchemaVersion int           `json:"schema_version"`
	ID            string        `json:"request_id"`
	ProjectID     string        `json:"project_id"`
	RequesterID   string        `json:"requester_member_id"`
	AgentID       string        `json:"agent_id"`
	Status        RequestStatus `json:"status"`
	BodyBytes     int           `json:"body_bytes"`
	BodySHA256    string        `json:"body_sha256"`
	CreatedAt     time.Time     `json:"created_at"`
	UpdatedAt     time.Time     `json:"updated_at"`
}

func (r Request) Metadata(status RequestStatus, at time.Time) RequestMetadata {
	return RequestMetadata{SchemaVersion: SchemaVersion, ID: r.ID, ProjectID: r.ProjectID, RequesterID: r.RequesterID, AgentID: r.AgentID, Status: status, BodyBytes: len(r.Body), BodySHA256: r.BodySHA256, CreatedAt: r.CreatedAt, UpdatedAt: at}
}

type Response struct {
	SchemaVersion int           `json:"schema_version"`
	RequestID     string        `json:"request_id"`
	Status        RequestStatus `json:"status"`
	Answer        []byte        `json:"answer"`
}

type RequestFailure struct {
	SchemaVersion int    `json:"schema_version"`
	RequestID     string `json:"request_id"`
	Code          string `json:"code"`
	Message       string `json:"message"`
	Retryable     bool   `json:"retryable"`
}

type AskResult struct {
	SchemaVersion int              `json:"schema_version"`
	Response      *Response        `json:"response,omitempty"`
	Metadata      *RequestMetadata `json:"metadata,omitempty"`
	Replayed      bool             `json:"replayed"`
}

type ProviderMessageType string

const (
	ProviderReady    ProviderMessageType = "ready"
	ProviderRequest  ProviderMessageType = "request"
	ProviderResponse ProviderMessageType = "response"
	ProviderFailure  ProviderMessageType = "failure"
	ProviderCancel   ProviderMessageType = "cancel"
	ProviderPing     ProviderMessageType = "ping"
	ProviderPong     ProviderMessageType = "pong"
)

type ProviderPayload struct {
	SchemaVersion int                 `json:"schema_version"`
	Type          ProviderMessageType `json:"type"`
	AgentID       string              `json:"agent_id,omitempty"`
	RequestID     string              `json:"request_id,omitempty"`
	Request       *Request            `json:"request,omitempty"`
	Response      *Response           `json:"response,omitempty"`
	Failure       *RequestFailure     `json:"failure,omitempty"`
}

func BodySHA256(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}
