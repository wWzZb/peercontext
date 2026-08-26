package codex

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const capabilityProbeTimeout = 30 * time.Second

type capabilityCommandRunner func(context.Context, string, []string, string, ...string) ([]byte, []byte, error)

func probeRuntimeCapabilities(codexPath, hostAuth string, runner capabilityCommandRunner) error {
	root, err := os.MkdirTemp("", "peerctx-capability-probe-")
	if err != nil {
		return capabilityProbeError("isolated_runtime_unavailable", "The runtime capability probe could not create a temporary directory", err)
	}
	defer os.RemoveAll(root)

	home := filepath.Join(root, "home")
	codexHome := filepath.Join(root, "codex-home")
	tmp := filepath.Join(root, "tmp")
	workspace := filepath.Join(root, "workspace")
	outside := filepath.Join(root, "outside")
	gitCommon := filepath.Join(root, "git-common")
	for _, dir := range []string{home, codexHome, tmp, workspace, outside, gitCommon} {
		if err = os.MkdirAll(dir, 0700); err != nil {
			return capabilityProbeError("isolated_runtime_unavailable", "The runtime capability probe could not prepare its temporary directories", err)
		}
	}
	if err = os.Symlink(hostAuth, filepath.Join(codexHome, "auth.json")); err != nil {
		return capabilityProbeError("codex_auth_unavailable", "The auth.json bridge could not be mounted for the runtime capability probe", err)
	}

	const allowed = "peerctx-capability-allowed"
	const metadata = "peerctx-capability-git-metadata"
	allowedPath := filepath.Join(workspace, "allowed.txt")
	outsidePath := filepath.Join(outside, "outside.txt")
	metadataPath := filepath.Join(gitCommon, "metadata.txt")
	for path, value := range map[string]string{allowedPath: allowed, outsidePath: "must-stay-private", metadataPath: metadata} {
		if err = os.WriteFile(path, []byte(value+"\n"), 0600); err != nil {
			return capabilityProbeError("isolated_runtime_unavailable", "The runtime capability probe could not prepare its canaries", err)
		}
	}

	env := isolatedEnvironment(home, codexHome, tmp)
	ctx, cancel := context.WithTimeout(context.Background(), capabilityProbeTimeout)
	defer cancel()

	stdout, stderr, runErr := runner(ctx, codexPath, env, workspace, "exec", "--help")
	if runErr != nil || !hasRequiredExecInterface(string(stdout)+string(stderr)) {
		return capabilityProbeError("codex_interface_unsupported", "Codex does not expose the exec interface required by isolated_runtime", runErr)
	}

	if err = os.WriteFile(filepath.Join(codexHome, "config.toml"), []byte(isolatedReadConfigFor(home, tmp)), 0600); err != nil {
		return capabilityProbeError("isolated_runtime_unavailable", "The runtime capability probe could not write its read profile", err)
	}
	if _, _, runErr = runner(ctx, codexPath, env, workspace, "login", "status"); runErr != nil {
		return capabilityProbeError("codex_auth_unavailable", "Codex could not use the isolated auth.json bridge", runErr)
	}
	if _, _, runErr = runner(ctx, codexPath, env, workspace, "debug", "prompt-input", "peerctx capability probe"); runErr != nil {
		return capabilityProbeError("codex_interface_unsupported", "Codex rejected the isolated_runtime configuration", runErr)
	}

	stdout, stderr, runErr = runner(ctx, codexPath, env, workspace,
		"sandbox", "--permission-profile", "peerctx-read", "--cd", workspace,
		"/bin/sh", "-c", readCapabilityScript, "peerctx-read-probe", outsidePath)
	if runErr != nil || strings.TrimSpace(string(stdout)) != allowed || pathExists(filepath.Join(workspace, "read-profile-write-probe")) {
		return capabilityProbeError("codex_read_isolation_probe_failed", "Codex did not enforce the read isolated_runtime filesystem boundary", capabilityCommandFailure(runErr, stdout, stderr))
	}

	if err = os.WriteFile(filepath.Join(codexHome, "config.toml"), []byte(isolatedWriteConfigFor(home, tmp, gitCommon)), 0600); err != nil {
		return capabilityProbeError("isolated_runtime_unavailable", "The runtime capability probe could not write its write profile", err)
	}
	stdout, stderr, runErr = runner(ctx, codexPath, env, workspace,
		"sandbox", "--permission-profile", "peerctx-write", "--cd", workspace,
		"/bin/sh", "-c", writeCapabilityScript, "peerctx-write-probe", gitCommon, outsidePath, outside)
	wantWriteOutput := allowed + "|" + metadata
	if runErr != nil || strings.TrimSpace(string(stdout)) != wantWriteOutput || !pathExists(filepath.Join(workspace, "write-profile-write-probe")) || pathExists(filepath.Join(gitCommon, "git-write-probe")) || pathExists(filepath.Join(outside, "outside-write-probe")) {
		return capabilityProbeError("codex_write_isolation_probe_failed", "Codex did not enforce the write isolated_runtime filesystem boundary", capabilityCommandFailure(runErr, stdout, stderr))
	}
	return nil
}

func hasRequiredExecInterface(help string) bool {
	for _, required := range []string{"--strict-config", "--ephemeral", "--ignore-rules", "--skip-git-repo-check", "--json"} {
		if !strings.Contains(help, required) {
			return false
		}
	}
	return true
}

func runCapabilityCommand(ctx context.Context, path string, env []string, directory string, args ...string) ([]byte, []byte, error) {
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Env = env
	cmd.Dir = directory
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.Bytes(), stderr.Bytes(), err
}

func capabilityProbeError(code, message string, err error) error {
	if err == nil {
		err = errors.New("capability assertion failed")
	}
	return &RuntimeError{Code: code, Message: message, Err: err}
}

func capabilityCommandFailure(runErr error, stdout, stderr []byte) error {
	detail := strings.TrimSpace(string(stderr))
	if len(detail) > 1000 {
		detail = detail[:1000] + "…"
	}
	return fmt.Errorf("command error=%v stdout=%q stderr=%q", runErr, strings.TrimSpace(string(stdout)), detail)
}

func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

const readCapabilityScript = `set -eu
allowed=$(/bin/cat ./allowed.txt)
if /bin/cat "$1" >/dev/null 2>&1; then exit 41; fi
if /usr/bin/touch ./read-profile-write-probe >/dev/null 2>&1; then exit 42; fi
/usr/bin/printf '%s' "$allowed"
`

const writeCapabilityScript = `set -eu
allowed=$(/bin/cat ./allowed.txt)
metadata=$(/bin/cat "$1/metadata.txt")
/usr/bin/touch ./write-profile-write-probe
if /usr/bin/touch "$1/git-write-probe" >/dev/null 2>&1; then exit 51; fi
if /bin/cat "$2" >/dev/null 2>&1; then exit 52; fi
if /usr/bin/touch "$3/outside-write-probe" >/dev/null 2>&1; then exit 53; fi
/usr/bin/printf '%s|%s' "$allowed" "$metadata"
`
