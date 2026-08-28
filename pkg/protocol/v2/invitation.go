package v2

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

type Invitation struct {
	SchemaVersion    int       `json:"schema_version"`
	ProtocolVersion  string    `json:"protocol_version"`
	ProjectID        string    `json:"project_id"`
	ProjectName      string    `json:"project_name"`
	Endpoints        []string  `json:"endpoints"`
	HostPublicKey    []byte    `json:"host_public_key"`
	InviteID         string    `json:"invite_id"`
	InvitePrivateKey []byte    `json:"invite_private_key"`
	ExpiresAt        time.Time `json:"expires_at"`
}

func (i Invitation) Validate(now time.Time) error {
	if i.SchemaVersion != SchemaVersion || i.ProtocolVersion != ProtocolVersion {
		return fmt.Errorf("%w: unsupported version", ErrInvalidInvitation)
	}
	if i.ProjectID == "" || strings.TrimSpace(i.ProjectName) == "" || i.InviteID == "" {
		return fmt.Errorf("%w: identity is incomplete", ErrInvalidInvitation)
	}
	if len(i.Endpoints) == 0 {
		return fmt.Errorf("%w: no LAN endpoint", ErrInvalidInvitation)
	}
	if len(i.HostPublicKey) != ed25519.PublicKeySize || len(i.InvitePrivateKey) != ed25519.PrivateKeySize {
		return fmt.Errorf("%w: key material is invalid", ErrInvalidInvitation)
	}
	if !i.ExpiresAt.After(now) {
		return fmt.Errorf("%w: invitation has expired", ErrInvitationExpired)
	}
	return nil
}

func EncodeInvitation(invitation Invitation) (string, error) {
	if err := invitation.Validate(time.Now().UTC()); err != nil {
		return "", err
	}
	data, err := json.Marshal(invitation)
	if err != nil {
		return "", err
	}
	return InvitationPrefix + base64.RawURLEncoding.EncodeToString(data), nil
}

func DecodeInvitation(value string, now time.Time) (Invitation, error) {
	var invitation Invitation
	if !strings.HasPrefix(value, InvitationPrefix) {
		return invitation, fmt.Errorf("%w: invalid prefix", ErrInvalidInvitation)
	}
	data, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(value, InvitationPrefix))
	if err != nil {
		return invitation, fmt.Errorf("%w: decode invitation: %v", ErrInvalidInvitation, err)
	}
	if err = json.Unmarshal(data, &invitation); err != nil {
		return invitation, fmt.Errorf("%w: parse invitation: %v", ErrInvalidInvitation, err)
	}
	if err = invitation.Validate(now); err != nil {
		return Invitation{}, err
	}
	return invitation, nil
}

type JoinRequest struct {
	SchemaVersion   int       `json:"schema_version"`
	ProjectID       string    `json:"project_id"`
	InviteID        string    `json:"invite_id"`
	MemberName      string    `json:"member_name"`
	MemberPublicKey []byte    `json:"member_public_key"`
	Method          string    `json:"method"`
	Path            string    `json:"path"`
	Nonce           string    `json:"nonce"`
	Timestamp       time.Time `json:"timestamp"`
	Signature       []byte    `json:"signature"`
}

func (j JoinRequest) signingBytes() []byte {
	return []byte(fmt.Sprintf("peerctx-join-v2\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s", j.ProjectID, j.InviteID, j.MemberName, base64.RawURLEncoding.EncodeToString(j.MemberPublicKey), j.Method, j.Path, j.Nonce, j.Timestamp.UTC().Format(time.RFC3339Nano)))
}

func (j *JoinRequest) Sign(privateKey ed25519.PrivateKey) {
	j.Signature = ed25519.Sign(privateKey, j.signingBytes())
}

func (j JoinRequest) Verify(publicKey ed25519.PublicKey, now time.Time, maxAge time.Duration) error {
	if j.SchemaVersion != SchemaVersion || j.ProjectID == "" || j.InviteID == "" || strings.TrimSpace(j.MemberName) == "" || j.Method == "" || j.Path == "" || j.Nonce == "" {
		return errors.New("join request is incomplete")
	}
	if len(j.MemberPublicKey) != ed25519.PublicKeySize {
		return errors.New("member public key is invalid")
	}
	if delta := now.Sub(j.Timestamp); delta > maxAge || delta < -maxAge {
		return fmt.Errorf("%w: join request exceeds limit", ErrClockSkew)
	}
	if !ed25519.Verify(publicKey, j.signingBytes(), j.Signature) {
		return fmt.Errorf("%w: join request", ErrSignatureInvalid)
	}
	return nil
}
