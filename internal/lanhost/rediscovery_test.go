package lanhost

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	protocolv2 "github.com/wWzZb/peercontext/pkg/protocol/v2"
)

func TestClientFallsBackToVerifiedDiscoveryAfterAddressChange(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "host.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	publicKey, privateKey, _ := ed25519.GenerateKey(rand.Reader)
	now := time.Now().UTC()
	project := protocolv2.Project{SchemaVersion: 2, ID: "project", Name: "Project", HostPublicKey: publicKey, CreatedAt: now}
	owner := protocolv2.Member{SchemaVersion: 2, ID: "owner", ProjectID: project.ID, Name: "Owner", PublicKey: publicKey, Owner: true, CreatedAt: now}
	if err := store.CreateProject(context.Background(), project, owner); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(NewServer(store, func(string) (HostIdentity, error) {
		return HostIdentity{MemberID: owner.ID, PrivateKey: privateKey}, nil
	}, func() []string { return nil }))
	defer server.Close()
	client := NewClient(Profile{ProjectID: project.ID, ProjectName: project.Name, MemberID: owner.ID, MemberName: owner.Name, HostPublicKey: publicKey, Endpoints: []string{"http://127.0.0.1:1"}}, privateKey)
	discoveryCalls := 0
	remembered := ""
	client.Discover = func(context.Context, string, ed25519.PublicKey) ([]string, error) {
		discoveryCalls++
		return []string{server.URL}, nil
	}
	client.OnEndpoint = func(endpoint string) { remembered = endpoint }
	agents, err := client.ListAgents(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if discoveryCalls != 1 || remembered != server.URL || len(agents) != 0 {
		t.Fatalf("discovery calls=%d remembered=%q agents=%#v", discoveryCalls, remembered, agents)
	}
}

func TestUnknownMemberCannotReadProjectAgents(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "host.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	hostPublic, hostPrivate, _ := ed25519.GenerateKey(rand.Reader)
	now := time.Now().UTC()
	project := protocolv2.Project{SchemaVersion: 2, ID: "project", Name: "Project", HostPublicKey: hostPublic, CreatedAt: now}
	owner := protocolv2.Member{SchemaVersion: 2, ID: "owner", ProjectID: project.ID, Name: "Owner", PublicKey: hostPublic, Owner: true, CreatedAt: now}
	if err := store.CreateProject(context.Background(), project, owner); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(NewServer(store, func(string) (HostIdentity, error) {
		return HostIdentity{MemberID: owner.ID, PrivateKey: hostPrivate}, nil
	}, func() []string { return nil }))
	defer server.Close()
	_, attackerPrivate, _ := ed25519.GenerateKey(rand.Reader)
	attacker := NewClient(Profile{ProjectID: project.ID, MemberID: "unknown-member", HostPublicKey: hostPublic, Endpoints: []string{server.URL}}, attackerPrivate)
	if _, err := attacker.ListAgents(context.Background()); err == nil {
		t.Fatal("unknown member read the Project Agent list")
	}
}
