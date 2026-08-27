package v2

import (
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"fmt"
	"time"
)

type SignedMessage struct {
	SchemaVersion int       `json:"schema_version"`
	ProjectID     string    `json:"project_id"`
	SenderID      string    `json:"sender_member_id"`
	Kind          string    `json:"kind"`
	Method        string    `json:"method"`
	Path          string    `json:"path"`
	ReplyTo       string    `json:"reply_to,omitempty"`
	Nonce         string    `json:"nonce"`
	Timestamp     time.Time `json:"timestamp"`
	Payload       []byte    `json:"payload"`
	Signature     []byte    `json:"signature"`
}

func (m SignedMessage) signingBytes() []byte {
	return []byte(fmt.Sprintf("peerctx-message-v2\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s", m.ProjectID, m.SenderID, m.Kind, m.Method, m.Path, m.ReplyTo, m.Nonce, m.Timestamp.UTC().Format(time.RFC3339Nano), BodySHA256(m.Payload)))
}

func NewSignedMessage(projectID, senderID, kind, nonce string, payload []byte, timestamp time.Time, privateKey ed25519.PrivateKey) SignedMessage {
	return NewSignedHTTPMessage(projectID, senderID, kind, "INTERNAL", "/"+kind, nonce, payload, timestamp, privateKey)
}

func NewSignedHTTPMessage(projectID, senderID, kind, method, path, nonce string, payload []byte, timestamp time.Time, privateKey ed25519.PrivateKey) SignedMessage {
	message := SignedMessage{SchemaVersion: SchemaVersion, ProjectID: projectID, SenderID: senderID, Kind: kind, Method: method, Path: path, Nonce: nonce, Timestamp: timestamp.UTC(), Payload: append([]byte(nil), payload...)}
	message.Signature = ed25519.Sign(privateKey, message.signingBytes())
	return message
}

func NewSignedReply(projectID, senderID, kind, method, path, replyTo, nonce string, payload []byte, timestamp time.Time, privateKey ed25519.PrivateKey) SignedMessage {
	message := SignedMessage{SchemaVersion: SchemaVersion, ProjectID: projectID, SenderID: senderID, Kind: kind, Method: method, Path: path, ReplyTo: replyTo, Nonce: nonce, Timestamp: timestamp.UTC(), Payload: append([]byte(nil), payload...)}
	message.Signature = ed25519.Sign(privateKey, message.signingBytes())
	return message
}

func (m SignedMessage) Verify(publicKey ed25519.PublicKey, now time.Time, maxAge time.Duration) error {
	if m.SchemaVersion != SchemaVersion || m.ProjectID == "" || m.SenderID == "" || m.Kind == "" || m.Method == "" || m.Path == "" || m.Nonce == "" {
		return errors.New("signed message is incomplete")
	}
	if len(publicKey) != ed25519.PublicKeySize {
		return errors.New("public key is invalid")
	}
	if delta := now.Sub(m.Timestamp); delta > maxAge || delta < -maxAge {
		return errors.New("signed message clock skew exceeds limit")
	}
	if !ed25519.Verify(publicKey, m.signingBytes(), m.Signature) {
		return errors.New("signed message signature is invalid")
	}
	return nil
}

func PublicKeyString(key ed25519.PublicKey) string { return base64.RawURLEncoding.EncodeToString(key) }
