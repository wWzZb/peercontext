package v2

import (
	"crypto/ed25519"
	"crypto/rand"
	"testing"
	"time"
)

func TestInvitationRoundTripAndExpiry(t *testing.T) {
	hostPublic, _, _ := ed25519.GenerateKey(rand.Reader)
	_, invitePrivate, _ := ed25519.GenerateKey(rand.Reader)
	now := time.Now().UTC()
	encoded, err := EncodeInvitation(Invitation{SchemaVersion: SchemaVersion, ProtocolVersion: ProtocolVersion, ProjectID: "prj_test", ProjectName: "test", Endpoints: []string{"http://192.168.1.2:7777"}, HostPublicKey: hostPublic, InviteID: "inv_test", InvitePrivateKey: invitePrivate, ExpiresAt: now.Add(time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeInvitation(encoded, now)
	if err != nil || decoded.ProjectID != "prj_test" {
		t.Fatalf("DecodeInvitation: %#v %v", decoded, err)
	}
	if _, err = DecodeInvitation(encoded, now.Add(2*time.Minute)); err == nil {
		t.Fatal("expired invitation was accepted")
	}
}

func TestJoinAndMessageSignaturesBindPayload(t *testing.T) {
	invitePublic, invitePrivate, _ := ed25519.GenerateKey(rand.Reader)
	memberPublic, memberPrivate, _ := ed25519.GenerateKey(rand.Reader)
	now := time.Now().UTC()
	join := JoinRequest{SchemaVersion: SchemaVersion, ProjectID: "prj", InviteID: "inv", MemberName: "alice", MemberPublicKey: memberPublic, Method: "POST", Path: "/v2/join", Nonce: "nonce", Timestamp: now}
	join.Sign(invitePrivate)
	if err := join.Verify(invitePublic, now, time.Minute); err != nil {
		t.Fatal(err)
	}
	join.MemberName = "mallory"
	if err := join.Verify(invitePublic, now, time.Minute); err == nil {
		t.Fatal("tampered join was accepted")
	}

	message := NewSignedMessage("prj", "mem", "ask", "nonce", []byte("body"), now, memberPrivate)
	if err := message.Verify(memberPublic, now, time.Minute); err != nil {
		t.Fatal(err)
	}
	message.Payload[0] = 'B'
	if err := message.Verify(memberPublic, now, time.Minute); err == nil {
		t.Fatal("tampered payload was accepted")
	}
	message = NewSignedMessage("prj", "mem", "ask", "nonce-2", []byte("body"), now, memberPrivate)
	message.Path = "/different"
	if err := message.Verify(memberPublic, now, time.Minute); err == nil {
		t.Fatal("tampered method/path binding was accepted")
	}
}

func TestSignatureClockSkewIsRejected(t *testing.T) {
	publicKey, privateKey, _ := ed25519.GenerateKey(rand.Reader)
	now := time.Now().UTC()
	message := NewSignedMessage("project", "member", "kind", "nonce", nil, now.Add(-3*time.Minute), privateKey)
	if err := message.Verify(publicKey, now, time.Minute); err == nil {
		t.Fatal("stale signed message was accepted")
	}
}
