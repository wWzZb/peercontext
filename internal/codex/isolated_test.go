package codex

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestParseFinalAgentMessageReturnsExactLastMessageBytes(t *testing.T) {
	jsonl := "{\"type\":\"thread.started\"}\n{\"type\":\"item.completed\",\"item\":{\"type\":\"agent_message\",\"text\":\"first\"}}\n{\"type\":\"item.completed\",\"item\":{\"type\":\"agent_message\",\"text\":\"line 1\\r\\nline 2\"}}\n{\"type\":\"turn.completed\"}\n"
	final, err := parseFinalAgentMessage(strings.NewReader(jsonl), 1024)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(final, []byte("line 1\r\nline 2")) {
		t.Fatalf("final = %q", final)
	}
}

func TestIsolatedWriteConfigAllowsOnlyDetachedWorkspaceAndReadOnlyGitMetadata(t *testing.T) {
	repository := filepath.Join(t.TempDir(), "repo")
	worktree := filepath.Join(t.TempDir(), "detached")
	if err := os.MkdirAll(repository, 0700); err != nil {
		t.Fatal(err)
	}
	gitTestRun(t, repository, "init", "-b", "main")
	gitTestRun(t, repository, "config", "user.name", "PeerContext Test")
	gitTestRun(t, repository, "config", "user.email", "peerctx@example.invalid")
	if err := os.WriteFile(filepath.Join(repository, "fixture.txt"), []byte("fixture\n"), 0600); err != nil {
		t.Fatal(err)
	}
	gitTestRun(t, repository, "add", "fixture.txt")
	gitTestRun(t, repository, "commit", "-m", "fixture")
	gitTestRun(t, repository, "worktree", "add", "--detach", worktree, "HEAD")
	commonBytes, err := exec.Command("git", "-C", worktree, "rev-parse", "--git-common-dir").Output()
	if err != nil {
		t.Fatal(err)
	}
	common := strings.TrimSpace(string(commonBytes))
	if !filepath.IsAbs(common) {
		common = filepath.Join(worktree, common)
	}
	common, _ = filepath.Abs(common)
	resolved, err := validateDetachedWorktree(worktree, common)
	if err != nil || resolved != common {
		t.Fatalf("validateDetachedWorktree = %q, %v", resolved, err)
	}
	config := isolatedWriteConfigFor("/clean/home", "/clean/tmp", common)
	for _, required := range []string{"default_permissions = \"peerctx-write\"", "\".\" = \"write\"", "\".git\" = \"read\"", strconv.Quote(common) + " = \"read\"", "enabled = false", "inherit = \"none\""} {
		if !strings.Contains(config, required) {
			t.Fatalf("write config missing %q", required)
		}
	}
	for _, forbidden := range []string{"danger-full-access", "unrestricted", "enabled = true"} {
		if strings.Contains(config, forbidden) {
			t.Fatalf("write config contains forbidden fallback %q", forbidden)
		}
	}
	if _, err := validateDetachedWorktree(repository, common); err == nil {
		t.Fatal("main checkout was accepted as a write workspace")
	}
	if _, err := validateDetachedWorktree(worktree, filepath.Join(t.TempDir(), "wrong-git-dir")); err == nil {
		t.Fatal("mismatched Git metadata boundary was accepted")
	}
}

func gitTestRun(t *testing.T, directory string, args ...string) {
	t.Helper()
	commandArgs := append([]string{"-C", directory}, args...)
	if output, err := exec.Command("git", commandArgs...).CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, output)
	}
}
func TestParseFinalAgentMessageRejectsMissingMalformedAndOversized(t *testing.T) {
	for name, input := range map[string]string{"missing": "{\"type\":\"turn.completed\"}\n", "malformed": "not-json\n", "oversized": "{\"type\":\"item.completed\",\"item\":{\"type\":\"agent_message\",\"text\":\"12345\"}}\n"} {
		t.Run(name, func(t *testing.T) {
			limit := 1024
			if name == "oversized" {
				limit = 4
			}
			_, err := parseFinalAgentMessage(strings.NewReader(input), limit)
			var runtimeErr *RuntimeError
			if !errors.As(err, &runtimeErr) {
				t.Fatalf("error = %v", err)
			}
			if name == "oversized" && runtimeErr.Code != "response_too_large" {
				t.Fatalf("code = %s", runtimeErr.Code)
			}
		})
	}
}
func TestIsolatedReadConfigHasNoHostFallbackAndEnvironmentIsMinimal(t *testing.T) {
	t.Setenv("HTTP_PROXY", "http://proxy-user:PEERCTX_PROXY_SECRET@proxy.invalid")
	config := isolatedReadConfigFor("/clean/home", "/clean/tmp")
	for _, required := range []string{"default_permissions = \"peerctx-read\"", "\":root\" = \"deny\"", "\".\" = \"read\"", "enabled = false"} {
		if !strings.Contains(config, required) {
			t.Fatalf("config missing %q", required)
		}
	}
	for _, forbidden := range []string{"danger-full-access", "unrestricted", "workspace-write"} {
		if strings.Contains(config, forbidden) {
			t.Fatalf("config contains forbidden fallback %q", forbidden)
		}
	}
	if !strings.Contains(config, "[shell_environment_policy]") || !strings.Contains(config, "inherit = \"none\"") || !strings.Contains(config, "PEERCTX_INBOUND_REQUEST") {
		t.Fatal("shell environment policy does not isolate child commands")
	}
	if strings.Contains(config, "PEERCTX_PROXY_SECRET") || strings.Contains(config, "HTTP_PROXY") {
		t.Fatal("proxy credential entered model command environment")
	}
	t.Setenv("PEERCTX_HOST_SECRET_CANARY", "must-not-pass")
	env := isolatedEnvironment("/clean/home", "/clean/codex", "/clean/tmp")
	combined := strings.Join(env, "\n")
	if strings.Contains(combined, "PEERCTX_HOST_SECRET_CANARY") || strings.Contains(combined, "must-not-pass") {
		t.Fatal("host environment canary entered isolated runtime")
	}
	if !strings.Contains(combined, "PEERCTX_INBOUND_REQUEST=1") {
		t.Fatal("recursive request marker missing")
	}
	_ = os.DevNull
}

func TestCompiledIsolationPoliciesPassDoctorInvariant(t *testing.T) {
	if err := ValidateIsolationPolicy(); err != nil {
		t.Fatal(err)
	}
}
