package lanhost

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"path/filepath"
	"testing"
	"time"

	protocolv2 "github.com/wWzZb/peercontext/pkg/protocol/v2"
)

func TestInvitationSingleUseAndMemberRemovalRevokesAgents(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "host.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	now := time.Now().UTC()
	hostPublic, _, _ := ed25519.GenerateKey(rand.Reader)
	project := protocolv2.Project{SchemaVersion: 2, ID: "project", Name: "Project", HostPublicKey: hostPublic, CreatedAt: now}
	owner := protocolv2.Member{SchemaVersion: 2, ID: "owner", ProjectID: project.ID, Name: "Owner", PublicKey: hostPublic, Owner: true, CreatedAt: now}
	if err := store.CreateProject(ctx, project, owner); err != nil {
		t.Fatal(err)
	}
	invitePublic, invitePrivate, _ := ed25519.GenerateKey(rand.Reader)
	if err := store.AddInvitation(ctx, project.ID, "invite", invitePublic, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	memberPublic, _, _ := ed25519.GenerateKey(rand.Reader)
	join := protocolv2.JoinRequest{SchemaVersion: 2, ProjectID: project.ID, InviteID: "invite", MemberName: "Member", MemberPublicKey: memberPublic, Method: "POST", Path: "/v2/join", Nonce: "join-nonce", Timestamp: now}
	join.Sign(invitePrivate)
	if _, err := store.ConsumeInvitation(ctx, join, now); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ConsumeInvitation(ctx, join, now); !errors.Is(err, ErrInviteConsumed) {
		t.Fatalf("second consume error = %v", err)
	}
	memberID := memberIDFromPublicKey(memberPublic)
	manifest := protocolv2.AgentManifest{SchemaVersion: 2, Name: "Member/repo"}
	agent, err := store.RegisterAgent(ctx, project.ID, memberID, "agent", manifest, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.RemoveMember(ctx, project.ID, owner.ID, memberID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Member(ctx, project.ID, memberID); !errors.Is(err, ErrMemberForbidden) {
		t.Fatalf("removed member error = %v", err)
	}
	if _, err := store.Agent(ctx, project.ID, agent.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("removed member Agent error = %v", err)
	}
}

func TestExpiredInvitationIsRejected(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "host.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	now := time.Now().UTC()
	hostPublic, _, _ := ed25519.GenerateKey(rand.Reader)
	project := protocolv2.Project{SchemaVersion: 2, ID: "project", Name: "Project", HostPublicKey: hostPublic, CreatedAt: now}
	owner := protocolv2.Member{SchemaVersion: 2, ID: "owner", ProjectID: project.ID, Name: "Owner", PublicKey: hostPublic, Owner: true, CreatedAt: now}
	if err := store.CreateProject(ctx, project, owner); err != nil {
		t.Fatal(err)
	}
	invitePublic, invitePrivate, _ := ed25519.GenerateKey(rand.Reader)
	_ = store.AddInvitation(ctx, project.ID, "expired", invitePublic, now.Add(-time.Second))
	memberPublic, _, _ := ed25519.GenerateKey(rand.Reader)
	join := protocolv2.JoinRequest{SchemaVersion: 2, ProjectID: project.ID, InviteID: "expired", MemberName: "Member", MemberPublicKey: memberPublic, Method: "POST", Path: "/v2/join", Nonce: "nonce", Timestamp: now}
	join.Sign(invitePrivate)
	if _, err := store.ConsumeInvitation(ctx, join, now); !errors.Is(err, ErrInviteExpired) {
		t.Fatalf("expired invitation error = %v", err)
	}
}
