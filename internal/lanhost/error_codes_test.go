package lanhost

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"testing"
	"time"

	"github.com/wWzZb/peercontext/internal/failure"
	protocolv2 "github.com/wWzZb/peercontext/pkg/protocol/v2"
)

func TestHostFailureCodesPreserveSecurityCause(t *testing.T) {
	tests := []struct {
		err  error
		code string
	}{
		{ErrInviteExpired, "invite_expired"},
		{ErrInviteConsumed, "invite_consumed"},
		{ErrInviteInvalid, "invalid_invitation"},
		{ErrRequestReplayed, "request_replayed"},
		{protocolv2.ErrClockSkew, "clock_skew"},
		{protocolv2.ErrSignatureInvalid, "signature_invalid"},
		{ErrMemberForbidden, "authorization_failed"},
	}
	for _, test := range tests {
		result := rpcFailureFor("fallback", test.err, true)
		if result.Error == nil || result.Error.Code != test.code || result.Error.Retryable {
			t.Fatalf("%v => %#v", test.err, result.Error)
		}
	}
}

func TestClientReturnsProjectHostOfflineWithoutEndpoints(t *testing.T) {
	_, privateKey, _ := ed25519.GenerateKey(rand.Reader)
	client := NewClient(Profile{ProjectID: "project", MemberID: "member"}, privateKey)
	err := client.RPC(context.Background(), KindAgentsList, struct{}{}, nil)
	var structured *failure.Error
	if !errors.As(err, &structured) || structured.Code != "project_host_offline" || !structured.Retryable {
		t.Fatalf("error = %#v", err)
	}
}

func TestAuthenticationDistinguishesReplayClockAndSignature(t *testing.T) {
	publicKey, privateKey, _ := ed25519.GenerateKey(rand.Reader)
	now := time.Now().UTC()
	store, server, project, member := testAuthServer(t, publicKey, now)
	defer store.Close()

	valid := protocolv2.NewSignedHTTPMessage(project.ID, member.ID, KindAgentsList, "POST", "/v2/rpc", "nonce", nil, now, privateKey)
	if _, err := server.authenticate(context.Background(), valid, "POST", "/v2/rpc"); err != nil {
		t.Fatal(err)
	}
	if _, err := server.authenticate(context.Background(), valid, "POST", "/v2/rpc"); !errors.Is(err, ErrRequestReplayed) {
		t.Fatalf("replay error = %v", err)
	}
}

func testAuthServer(t *testing.T, publicKey ed25519.PublicKey, now time.Time) (*Store, *Server, protocolv2.Project, protocolv2.Member) {
	t.Helper()
	store, err := OpenStore(t.TempDir() + "/host.sqlite")
	if err != nil {
		t.Fatal(err)
	}
	project := protocolv2.Project{SchemaVersion: 2, ID: "project", Name: "Project", HostPublicKey: publicKey, CreatedAt: now}
	member := protocolv2.Member{SchemaVersion: 2, ID: "member", ProjectID: project.ID, Name: "Member", PublicKey: publicKey, CreatedAt: now}
	if err := store.CreateProject(context.Background(), project, member); err != nil {
		store.Close()
		t.Fatal(err)
	}
	server := NewServer(store, func(string) (HostIdentity, error) { return HostIdentity{}, nil }, func() []string { return nil })
	server.Now = func() time.Time { return now }
	return store, server, project, member
}
