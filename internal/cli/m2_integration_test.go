package cli

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wWzZb/peercontext/internal/relay"
	"github.com/wWzZb/peercontext/pkg/clioutput"
)

func TestM2CLIProjectAgentACLAndCredentialFlow(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "relay.sqlite")
	var relayLogs bytes.Buffer
	store, err := relay.OpenStore(dbPath, slog.New(slog.NewJSONHandler(&relayLogs, nil)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	server, err := relay.NewServer(store, slog.New(slog.NewJSONHandler(&relayLogs, nil)))
	if err != nil {
		t.Fatal(err)
	}
	httpServer := httptest.NewServer(server.Handler())
	t.Cleanup(httpServer.Close)

	root := t.TempDir()
	ownerConfig := filepath.Join(root, "owner-config")
	memberConfig := filepath.Join(root, "member-config")
	ownerCredential := filepath.Join(root, "owner.credential")
	memberCredential := filepath.Join(root, "member.credential")

	t.Setenv("PEERCTX_CONFIG_DIR", ownerConfig)
	created := runCLISuccess(t, []string{"project", "create", "--relay", httpServer.URL, "--name", "payments", "--owner", "alice", "--credential-file", ownerCredential})
	projectID := nestedString(t, created, "project", "project_id")
	if strings.Contains(string(mustJSON(t, created)), "credential_token") {
		t.Fatal("project create CLI output exposed credential token")
	}

	invite := runCLISuccess(t, []string{"project", "invite", "create", "--expires-in", "1h"})
	inviteToken := stringField(t, invite, "invite_token")

	t.Setenv("PEERCTX_CONFIG_DIR", memberConfig)
	joined := runCLISuccess(t, []string{"project", "join", "--relay", httpServer.URL, "--invite-token", inviteToken, "--member", "bob", "--credential-file", memberCredential})
	memberID := nestedString(t, joined, "member", "member_id")
	if nestedString(t, joined, "project", "project_id") != projectID {
		t.Fatal("joined a different project")
	}
	status := runCLISuccess(t, []string{"credential", "status"})
	if nestedString(t, status, "member", "member_id") != memberID {
		t.Fatal("credential status returned another member")
	}
	runCLISuccess(t, []string{"credential", "rotate"})
	runCLISuccess(t, []string{"credential", "status"})

	const repositoryCanary = "/private/provider/PEERCTX_LOCAL_REPOSITORY_CANARY"
	t.Setenv("PEERCTX_CONFIG_DIR", ownerConfig)
	registered := runCLISuccess(t, []string{"agent", "register", "--repository", repositoryCanary, "--name", "payments-backend", "--summary", "Payments contracts", "--tags", "backend,payments", "--capabilities", "API contract,approved changes", "--modes", "read,write"})
	agentID := stringField(t, registered, "agent_id")
	runCLISuccess(t, []string{"agent", "access", "grant", agentID, "--member", memberID, "--modes", "read"})

	t.Setenv("PEERCTX_CONFIG_DIR", memberConfig)
	agents := runCLISuccess(t, []string{"agent", "list"})
	agentList, ok := agents["agents"].([]any)
	if !ok || len(agentList) != 1 {
		t.Fatalf("agents = %#v", agents["agents"])
	}
	listed := agentList[0].(map[string]any)
	if listed["online"] != false {
		t.Fatalf("offline agent = %#v", listed)
	}
	if strings.Contains(string(mustJSON(t, agents)), repositoryCanary) {
		t.Fatal("public Agent manifest exposed repository path")
	}

	database, err := os.ReadFile(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	combined := string(database) + relayLogs.String()
	for _, secret := range []string{inviteToken, strings.TrimSpace(readFile(t, ownerCredential)), strings.TrimSpace(readFile(t, memberCredential)), repositoryCanary} {
		if strings.Contains(combined, secret) {
			t.Fatalf("Relay database/logs contain sensitive value %q", secret)
		}
	}

	info, err := os.Stat(memberCredential)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("credential mode = %o, want 600", info.Mode().Perm())
	}
	runCLISuccess(t, []string{"credential", "revoke"})
	if _, err := os.Stat(memberCredential); !os.IsNotExist(err) {
		t.Fatalf("credential file after self revoke: %v", err)
	}
}

func runCLISuccess(t *testing.T, args []string) map[string]any {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := Run(args, bytes.NewReader(nil), &stdout, &stderr)
	if code != clioutput.ExitOK {
		t.Fatalf("Run(%v) code=%d stderr=%s", args, code, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("Run(%v) stderr=%s", args, stderr.String())
	}
	var envelope struct {
		OK   bool           `json:"ok"`
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatalf("Run(%v) output: %v\n%s", args, err, stdout.String())
	}
	if !envelope.OK {
		t.Fatalf("Run(%v) ok=false", args)
	}
	return envelope.Data
}

func nestedString(t *testing.T, value map[string]any, object, field string) string {
	t.Helper()
	nested, ok := value[object].(map[string]any)
	if !ok {
		t.Fatalf("%s = %#v", object, value[object])
	}
	return stringField(t, nested, field)
}
func stringField(t *testing.T, value map[string]any, field string) string {
	t.Helper()
	result, ok := value[field].(string)
	if !ok || result == "" {
		t.Fatalf("%s = %#v", field, value[field])
	}
	return result
}
func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
