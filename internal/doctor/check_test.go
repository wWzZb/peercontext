package doctor

import (
	"encoding/json"
	"log/slog"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/wWzZb/peercontext/internal/codex"
	"github.com/wWzZb/peercontext/internal/localstate"
	"github.com/wWzZb/peercontext/internal/relay"
)

func TestDoctorChecksRuntimeRelayCredentialRepositoryWithoutLeakingSecretsOrPaths(t *testing.T) {
	configRoot := t.TempDir()
	manager, err := localstate.NewManager(configRoot)
	if err != nil {
		t.Fatal(err)
	}
	store, err := relay.OpenStore(filepath.Join(t.TempDir(), "relay.sqlite"), slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	server, _ := relay.NewServer(store, slog.Default())
	httpServer := httptest.NewServer(server.Handler())
	t.Cleanup(httpServer.Close)

	const tokenCanary = "PEERCTX_DOCTOR_TOKEN_CANARY_4ab872"
	credentialPath := filepath.Join(t.TempDir(), "secret-credential-file")
	if err = manager.PrepareCredentialStore(credentialPath); err != nil {
		t.Fatal(err)
	}
	profile := localstate.Profile{ProjectID: "prj_doctor", ProjectName: "doctor", RelayURL: httpServer.URL, MemberID: "mem_doctor", MemberName: "owner"}
	if err = manager.PutProfile(profile, tokenCanary, credentialPath); err != nil {
		t.Fatal(err)
	}
	repository := filepath.Join(t.TempDir(), "private-repository-canary")
	if err = os.MkdirAll(repository, 0700); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command("git", "-C", repository, "init").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, output)
	}
	if err = manager.PutAgent(localstate.LocalAgent{AgentID: "agt_doctor", ProjectID: profile.ProjectID, Repository: repository}); err != nil {
		t.Fatal(err)
	}

	fakeBin := t.TempDir()
	fakeCodex := filepath.Join(fakeBin, "codex")
	script := "#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then echo '" + codex.VerifiedCodexVersion + "'; exit 0; fi\nexit 1\n"
	if err = os.WriteFile(fakeCodex, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))
	codexHome := t.TempDir()
	if err = os.WriteFile(filepath.Join(codexHome, "auth.json"), []byte("{}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEX_HOME", codexHome)

	report := Run(t.Context(), manager)
	if runtime.GOOS == "darwin" && runtime.GOARCH == "arm64" && !report.Healthy {
		t.Fatalf("doctor report = %#v", report)
	}
	if (runtime.GOOS != "darwin" || runtime.GOARCH != "arm64") && report.Healthy {
		t.Fatal("ungated platform reported healthy")
	}
	encoded, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{tokenCanary, credentialPath, repository, codexHome} {
		if strings.Contains(string(encoded), secret) {
			t.Fatalf("doctor report leaked secret/path %q: %s", secret, encoded)
		}
	}
}

func TestRelayTLSCheckRequiresTLSBeyondLiteralLoopback(t *testing.T) {
	for _, value := range []string{"http://127.0.0.1:7777", "http://[::1]:7777", "http://localhost:7777", "https://relay.example.com"} {
		if err := relayTLSCheck(value); err != nil {
			t.Fatalf("relayTLSCheck(%q): %v", value, err)
		}
	}
	for _, value := range []string{"http://relay.example.com", "ws://127.0.0.1:7777", "not-a-url"} {
		if err := relayTLSCheck(value); err == nil {
			t.Fatalf("relayTLSCheck(%q) accepted unsafe URL", value)
		}
	}
}
