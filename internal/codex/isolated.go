package codex

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"

	protocolv2 "github.com/wWzZb/peercontext/pkg/protocol/v2"
)

type RuntimeError struct {
	Code    string
	Message string
	Err     error
}

func (e *RuntimeError) Error() string {
	if e.Err != nil {
		return e.Message + ": " + e.Err.Error()
	}
	return e.Message
}
func (e *RuntimeError) Unwrap() error { return e.Err }

type IsolatedAdapter struct {
	codexPath  string
	hostAuth   string
	tempParent string
}

func NewIsolatedAdapter() (*IsolatedAdapter, error) {
	if !SupportedPlatform() {
		return nil, &RuntimeError{Code: "runtime_platform_ungated", Message: "This platform has not passed the isolated_runtime gate"}
	}
	codexPath, err := exec.LookPath("codex")
	if err != nil {
		return nil, &RuntimeError{Code: "codex_not_found", Message: "Codex CLI is not installed", Err: err}
	}
	hostHome, err := os.UserHomeDir()
	if err != nil {
		return nil, &RuntimeError{Code: "host_home_unavailable", Message: "The host home directory is unavailable", Err: err}
	}
	hostCodexHome := os.Getenv("CODEX_HOME")
	if hostCodexHome == "" {
		hostCodexHome = filepath.Join(hostHome, ".codex")
	}
	hostAuth := filepath.Join(hostCodexHome, "auth.json")
	info, err := os.Stat(hostAuth)
	if err != nil || !info.Mode().IsRegular() {
		return nil, &RuntimeError{Code: "codex_auth_unavailable", Message: "The verified auth.json bridge is unavailable", Err: err}
	}
	if err = probeRuntimeCapabilities(codexPath, hostAuth, runCapabilityCommand); err != nil {
		return nil, err
	}
	return &IsolatedAdapter{codexPath: codexPath, hostAuth: hostAuth}, nil
}

func SupportedPlatform() bool { return runtime.GOOS == "darwin" && runtime.GOARCH == "arm64" }

// ValidateIsolationPolicy checks the compiled permission templates used by
// doctor. It validates configuration invariants without starting Codex or
// reading any repository.
func ValidateIsolationPolicy() error {
	read := isolatedReadConfigFor("/peerctx/clean-home", "/peerctx/clean-tmp")
	for _, required := range []string{"\":root\" = \"deny\"", "enabled = false", "inherit = \"none\"", "PEERCTX_INBOUND_REQUEST"} {
		if !strings.Contains(read, required) {
			return fmt.Errorf("read isolation policy is missing %q", required)
		}
	}
	for _, forbidden := range []string{"danger-full-access", "unrestricted", "enabled = true", "inherit = \"all\"", "inherit = \"core\"", "peerctx-write"} {
		if strings.Contains(read, forbidden) {
			return fmt.Errorf("read isolation policy contains forbidden value %q", forbidden)
		}
	}
	for _, required := range []string{"\".\" = \"read\"", "default_permissions = \"peerctx-read\""} {
		if !strings.Contains(read, required) {
			return fmt.Errorf("read isolation policy is missing %q", required)
		}
	}
	return nil
}

func (a *IsolatedAdapter) Run(ctx context.Context, invocation Invocation) (Result, error) {
	if invocation.Stdin == nil {
		return Result{}, &RuntimeError{Code: "codex_stdin_required", Message: "Codex stdin is required"}
	}
	workspace, err := filepath.EvalSymlinks(invocation.Workspace)
	if err != nil {
		return Result{}, &RuntimeError{Code: "agent_repository_unavailable", Message: "The Agent repository is unavailable", Err: err}
	}
	info, err := os.Stat(workspace)
	if err != nil || !info.IsDir() {
		return Result{}, &RuntimeError{Code: "agent_repository_unavailable", Message: "The Agent repository is unavailable", Err: err}
	}
	root, err := os.MkdirTemp(a.tempParent, "peerctx-request-")
	if err != nil {
		return Result{}, &RuntimeError{Code: "isolated_runtime_unavailable", Message: "The isolated runtime directory could not be created", Err: err}
	}
	defer os.RemoveAll(root)
	home := filepath.Join(root, "home")
	codexHome := filepath.Join(root, "codex-home")
	tmp := filepath.Join(root, "tmp")
	runtimeDir := filepath.Join(root, "runtime")
	for _, dir := range []string{home, codexHome, tmp, runtimeDir} {
		if err = os.MkdirAll(dir, 0700); err != nil {
			return Result{}, &RuntimeError{Code: "isolated_runtime_unavailable", Message: "The isolated runtime directory could not be prepared", Err: err}
		}
	}
	if err = os.Symlink(a.hostAuth, filepath.Join(codexHome, "auth.json")); err != nil {
		return Result{}, &RuntimeError{Code: "isolated_runtime_unavailable", Message: "The verified auth.json bridge could not be mounted", Err: err}
	}
	config := isolatedReadConfigFor(home, tmp)
	if err = os.WriteFile(filepath.Join(codexHome, "config.toml"), []byte(config), 0600); err != nil {
		return Result{}, &RuntimeError{Code: "isolated_runtime_unavailable", Message: "The isolated Codex config could not be written", Err: err}
	}

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	cmd := exec.CommandContext(runCtx, a.codexPath, "exec", "--strict-config", "--ephemeral", "--ignore-rules", "--skip-git-repo-check", "--json", "-")
	cmd.Dir = workspace
	cmd.Env = isolatedEnvironment(home, codexHome, tmp)
	cmd.Stdin = invocation.Stdin
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return Result{}, &RuntimeError{Code: "codex_execution_failed", Message: "Codex stdout could not be opened", Err: err}
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return Result{}, &RuntimeError{Code: "codex_execution_failed", Message: "Codex stderr could not be opened", Err: err}
	}
	if err = cmd.Start(); err != nil {
		return Result{}, &RuntimeError{Code: "codex_execution_failed", Message: "Codex could not be started", Err: err}
	}
	stderrDone := make(chan struct{})
	go func() { _, _ = io.Copy(io.Discard, stderr); close(stderrDone) }()
	final, parseErr := parseFinalAgentMessage(stdout, protocolv2.MaxResponseBodyBytes)
	if parseErr != nil {
		cancel()
	}
	waitErr := cmd.Wait()
	<-stderrDone
	if parseErr != nil {
		return Result{}, parseErr
	}
	if ctx.Err() != nil {
		return Result{}, ctx.Err()
	}
	if waitErr != nil {
		return Result{}, &RuntimeError{Code: "codex_execution_failed", Message: "Codex exited without a successful final answer", Err: waitErr}
	}
	return Result{FinalMessage: final}, nil
}

const isolatedReadConfig = `check_for_update_on_startup = false
default_permissions = "peerctx-read"

[analytics]
enabled = false

[features]
apps = false
browser_use = false
computer_use = false
hooks = false
multi_agent = false
plugins = false

[permissions.peerctx-read.filesystem]
":root" = "deny"
":minimal" = "read"
":tmpdir" = "write"

[permissions.peerctx-read.filesystem.":workspace_roots"]
"." = "read"

[permissions.peerctx-read.network]
enabled = false
`

func isolatedReadConfigFor(home, tmp string) string {
	return isolatedConfigFor(home, tmp)
}

func isolatedConfigFor(home, tmp string) string {
	pathValue := os.Getenv("PATH")
	if pathValue == "" {
		pathValue = "/usr/bin:/bin"
	}
	values := map[string]string{"PATH": pathValue, "HOME": home, "USERPROFILE": home, "TMPDIR": tmp, "GIT_CONFIG_GLOBAL": os.DevNull, "GIT_CONFIG_NOSYSTEM": "1", "NO_COLOR": "1", "TERM": "dumb", "PEERCTX_INBOUND_REQUEST": "1"}
	for _, key := range []string{"LANG", "LC_ALL", "SHELL"} {
		if value := os.Getenv(key); value != "" {
			values[key] = value
		}
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var entries []string
	for _, key := range keys {
		entries = append(entries, strconv.Quote(key)+" = "+strconv.Quote(values[key]))
	}
	return isolatedReadConfig + "\n[shell_environment_policy]\ninherit = \"none\"\nset = { " + strings.Join(entries, ", ") + " }\n"
}

func isolatedEnvironment(home, codexHome, tmp string) []string {
	values := map[string]string{"HOME": home, "USERPROFILE": home, "CODEX_HOME": codexHome, "TMPDIR": tmp, "GIT_CONFIG_GLOBAL": os.DevNull, "GIT_CONFIG_NOSYSTEM": "1", "NO_COLOR": "1", "TERM": "dumb", "PEERCTX_INBOUND_REQUEST": "1"}
	for _, key := range []string{"PATH", "USER", "LOGNAME", "LANG", "LC_ALL", "SHELL", "SSL_CERT_FILE", "SSL_CERT_DIR", "HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "NO_PROXY", "http_proxy", "https_proxy", "all_proxy", "no_proxy"} {
		if value := os.Getenv(key); value != "" {
			values[key] = value
		}
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	env := make([]string, 0, len(keys))
	for _, key := range keys {
		env = append(env, key+"="+values[key])
	}
	return env
}

func parseFinalAgentMessage(reader io.Reader, maxBytes int) ([]byte, error) {
	decoder := json.NewDecoder(bufio.NewReader(reader))
	var final []byte
	for {
		var event struct {
			Type string `json:"type"`
			Item struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"item"`
		}
		err := decoder.Decode(&event)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, &RuntimeError{Code: "codex_protocol_error", Message: "Codex JSONL could not be decoded", Err: err}
		}
		if event.Type == "item.completed" && event.Item.Type == "agent_message" {
			if len(event.Item.Text) > maxBytes {
				return nil, &RuntimeError{Code: "response_too_large", Message: fmt.Sprintf("Codex final answer exceeds %d bytes", maxBytes)}
			}
			final = bytes.Clone([]byte(event.Item.Text))
		}
	}
	if final == nil {
		return nil, &RuntimeError{Code: "codex_protocol_error", Message: "Codex JSONL did not contain a final Agent message"}
	}
	return final, nil
}
