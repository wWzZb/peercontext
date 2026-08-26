package localstate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExplicitCredentialFileIsOptInPrivateAndNeverOverwritten(t *testing.T) {
	manager, err := NewManager(filepath.Join(t.TempDir(), "config"))
	if err != nil {
		t.Fatal(err)
	}
	credentialPath := filepath.Join(t.TempDir(), "credentials", "project.token")
	if err := manager.PrepareCredentialStore(credentialPath); err != nil {
		t.Fatalf("PrepareCredentialStore: %v", err)
	}
	profile := Profile{ProjectID: "prj_test", ProjectName: "test", RelayURL: "http://127.0.0.1:7777", MemberID: "mem_test", MemberName: "alice"}
	if err := manager.PutProfile(profile, "first-token", credentialPath); err != nil {
		t.Fatalf("PutProfile: %v", err)
	}
	info, err := os.Stat(credentialPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("credential mode = %o", info.Mode().Perm())
	}
	_, current, err := manager.Current()
	if err != nil {
		t.Fatal(err)
	}
	token, err := manager.Token(current)
	if err != nil || token != "first-token" {
		t.Fatalf("Token = %q, %v", token, err)
	}
	if err := manager.PrepareCredentialStore(credentialPath); err == nil {
		t.Fatal("preflight accepted an existing credential file")
	}
	if err := manager.PutProfile(profile, "replacement-token", credentialPath); err == nil {
		t.Fatal("PutProfile overwrote an existing credential file")
	}
	data, _ := os.ReadFile(credentialPath)
	if strings.TrimSpace(string(data)) != "first-token" {
		t.Fatalf("credential was overwritten: %q", data)
	}
	if err := manager.ReplaceToken(current, "rotated-token"); err != nil {
		t.Fatalf("ReplaceToken: %v", err)
	}
	data, _ = os.ReadFile(credentialPath)
	if strings.TrimSpace(string(data)) != "rotated-token" {
		t.Fatalf("rotation was not saved: %q", data)
	}
}

func TestRepositoryPathStaysOnlyInLocalAgentState(t *testing.T) {
	manager, err := NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.PutAgent(LocalAgent{AgentID: "agt_test", ProjectID: "prj_test", Repository: "relative/repository"}); err != nil {
		t.Fatal(err)
	}
	agent, err := manager.Agent("prj_test", "agt_test")
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(agent.Repository) {
		t.Fatalf("repository = %q, want absolute local path", agent.Repository)
	}
}
