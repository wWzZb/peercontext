package relay

import (
	"context"
	"errors"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	protocolv1 "github.com/wWzZb/peercontext/pkg/protocol/v1"
)

func TestProjectInviteCredentialAndOwnerLifecycle(t *testing.T) {
	t.Parallel()
	store := openTestStore(t)
	ctx := context.Background()

	project, owner, ownerToken, err := store.CreateProject(ctx, "payments", "alice")
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	ownerPrincipal, err := store.Authenticate(ctx, ownerToken)
	if err != nil {
		t.Fatalf("Authenticate(owner): %v", err)
	}
	if !ownerPrincipal.Member.Owner || ownerPrincipal.Project.ID != project.ID {
		t.Fatalf("owner principal = %#v", ownerPrincipal)
	}

	invite, inviteToken, err := store.CreateInvite(ctx, ownerPrincipal, time.Hour)
	if err != nil {
		t.Fatalf("CreateInvite: %v", err)
	}
	if inviteToken == "" || invite.ExpiresAt.IsZero() {
		t.Fatal("invite did not return its one-time token and expiry")
	}
	_, bob, bobToken, err := store.JoinInvite(ctx, inviteToken, "bob")
	if err != nil {
		t.Fatalf("JoinInvite: %v", err)
	}
	if _, _, _, err := store.JoinInvite(ctx, inviteToken, "charlie"); !errors.Is(err, ErrInviteConsumed) {
		t.Fatalf("invite replay error = %v, want ErrInviteConsumed", err)
	}
	if _, _, _, err := store.JoinInvite(ctx, inviteToken+"x", "charlie"); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("invalid invite error = %v", err)
	}

	bobPrincipal, err := store.Authenticate(ctx, bobToken)
	if err != nil {
		t.Fatalf("Authenticate(bob): %v", err)
	}
	if _, _, err := store.CreateInvite(ctx, bobPrincipal, time.Hour); !errors.Is(err, ErrForbidden) {
		t.Fatalf("member CreateInvite error = %v", err)
	}
	if err := store.RemoveMember(ctx, ownerPrincipal, owner.ID); !errors.Is(err, ErrLastOwner) {
		t.Fatalf("remove final owner error = %v", err)
	}

	promoted, err := store.PromoteMember(ctx, ownerPrincipal, bob.ID)
	if err != nil || !promoted.Owner {
		t.Fatalf("PromoteMember = %#v, %v", promoted, err)
	}
	if err := store.RemoveMember(ctx, ownerPrincipal, owner.ID); err != nil {
		t.Fatalf("RemoveMember with second owner: %v", err)
	}
	if _, err := store.Authenticate(ctx, ownerToken); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("removed member credential error = %v", err)
	}

	bobPrincipal, err = store.Authenticate(ctx, bobToken)
	if err != nil {
		t.Fatalf("Authenticate(promoted bob): %v", err)
	}
	credential, rotatedToken, err := store.RotateCredential(ctx, bobPrincipal)
	if err != nil {
		t.Fatalf("RotateCredential: %v", err)
	}
	if credential.ID == bobPrincipal.Credential.ID || rotatedToken == bobToken {
		t.Fatal("rotation reused credential identity or token")
	}
	if _, err := store.Authenticate(ctx, bobToken); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("old token after rotation error = %v", err)
	}
	rotatedPrincipal, err := store.Authenticate(ctx, rotatedToken)
	if err != nil {
		t.Fatalf("Authenticate(rotated): %v", err)
	}
	if err := store.RevokeCredential(ctx, rotatedPrincipal, ""); err != nil {
		t.Fatalf("RevokeCredential: %v", err)
	}
	if _, err := store.Authenticate(ctx, rotatedToken); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("revoked token error = %v", err)
	}
}

func TestAgentRegistrationManifestAndModeACL(t *testing.T) {
	t.Parallel()
	store := openTestStore(t)
	ctx := context.Background()
	_, owner, ownerToken, err := store.CreateProject(ctx, "orders", "alice")
	if err != nil {
		t.Fatal(err)
	}
	principal, _ := store.Authenticate(ctx, ownerToken)
	invite, inviteToken, _ := store.CreateInvite(ctx, principal, time.Hour)
	_ = invite
	_, member, _, err := store.JoinInvite(ctx, inviteToken, "bob")
	if err != nil {
		t.Fatal(err)
	}

	manifest := protocolv1.AgentManifest{SchemaVersion: 1, Name: "orders-backend", Summary: "Order contracts", Tags: []string{"orders"}, Capabilities: []string{"API contract"}, Modes: []protocolv1.RequestMode{protocolv1.ModeRead, protocolv1.ModeWrite}}
	agent, err := store.RegisterAgent(ctx, principal, manifest)
	if err != nil {
		t.Fatalf("RegisterAgent: %v", err)
	}
	if agent.OwnerMemberID != owner.ID || agent.Manifest.Name != manifest.Name {
		t.Fatalf("agent = %#v", agent)
	}
	readOnly := manifest
	readOnly.Name = "read-only"
	readOnly.Modes = []protocolv1.RequestMode{protocolv1.ModeRead}
	readOnlyAgent, err := store.RegisterAgent(ctx, principal, readOnly)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetACL(ctx, principal, readOnlyAgent.ID, member.ID, []protocolv1.RequestMode{protocolv1.ModeWrite}, true); err == nil {
		t.Fatal("granted write ACL to an Agent that does not advertise write mode")
	}
	acl, err := store.SetACL(ctx, principal, agent.ID, member.ID, []protocolv1.RequestMode{protocolv1.ModeRead}, true)
	if err != nil {
		t.Fatalf("grant read: %v", err)
	}
	if len(acl.Modes) != 1 || acl.Modes[0] != protocolv1.ModeRead {
		t.Fatalf("ACL = %#v", acl)
	}
	memberPrincipal := Principal{Project: principal.Project, Member: member}
	read, _ := store.HasAccess(ctx, memberPrincipal, agent.ID, protocolv1.ModeRead)
	write, _ := store.HasAccess(ctx, memberPrincipal, agent.ID, protocolv1.ModeWrite)
	if !read || write {
		t.Fatalf("access read=%v write=%v", read, write)
	}
	if _, err := store.SetACL(ctx, principal, agent.ID, member.ID, []protocolv1.RequestMode{protocolv1.ModeWrite}, true); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetACL(ctx, principal, agent.ID, member.ID, []protocolv1.RequestMode{protocolv1.ModeRead}, false); err != nil {
		t.Fatal(err)
	}
	read, _ = store.HasAccess(ctx, memberPrincipal, agent.ID, protocolv1.ModeRead)
	write, _ = store.HasAccess(ctx, memberPrincipal, agent.ID, protocolv1.ModeWrite)
	if read || !write {
		t.Fatalf("access after update read=%v write=%v", read, write)
	}
}

func TestRequestReplayRequiresIdenticalBinding(t *testing.T) {
	t.Parallel()
	store := openTestStore(t)
	now := time.Now().UTC()
	body := []byte("raw request")
	request := protocolv1.Request{SchemaVersion: 1, ID: "req_replay", ProjectID: "prj_test", RequesterID: "mem_test", AgentID: "agt_test", Mode: protocolv1.ModeRead, Body: body, BodySHA256: protocolv1.BodySHA256(body), CreatedAt: now}
	metadata, replayed, err := store.BeginRequest(t.Context(), request, protocolv1.StatusRunning, now)
	if err != nil || replayed {
		t.Fatalf("BeginRequest(new) = %#v, %v, %v", metadata, replayed, err)
	}
	updated, err := store.UpdateRequestStatus(t.Context(), request.ID, protocolv1.StatusSucceeded, now.Add(time.Second))
	if err != nil || updated.Status != protocolv1.StatusSucceeded {
		t.Fatalf("UpdateRequestStatus = %#v, %v", updated, err)
	}
	existing, replayed, err := store.BeginRequest(t.Context(), request, protocolv1.StatusRunning, now.Add(2*time.Second))
	if err != nil || !replayed || existing.Status != protocolv1.StatusSucceeded {
		t.Fatalf("BeginRequest(replay) = %#v, %v, %v", existing, replayed, err)
	}
	request.Body = []byte("different")
	request.BodySHA256 = protocolv1.BodySHA256(request.Body)
	if _, _, err := store.BeginRequest(t.Context(), request, protocolv1.StatusRunning, now); !errors.Is(err, ErrReplayMismatch) {
		t.Fatalf("mismatched replay error = %v", err)
	}
}

func openTestStore(t *testing.T) *Store {
	t.Helper()
	store, err := OpenStore(filepath.Join(t.TempDir(), "relay.sqlite"), slog.Default())
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}
