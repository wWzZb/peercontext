package codex

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRuntimeCapabilityProbeChecksInterfaceAuthAndReadWriteBoundaries(t *testing.T) {
	hostAuth := filepath.Join(t.TempDir(), "auth.json")
	if err := os.WriteFile(hostAuth, []byte("{}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	var calls []string
	runner := func(_ context.Context, _ string, _ []string, directory string, args ...string) ([]byte, []byte, error) {
		calls = append(calls, strings.Join(args, " "))
		joined := strings.Join(args, " ")
		switch {
		case joined == "exec --help":
			return []byte("--strict-config --ephemeral --ignore-rules --skip-git-repo-check --json"), nil, nil
		case strings.Contains(joined, "login status"):
			return []byte("Logged in using ChatGPT"), nil, nil
		case strings.Contains(joined, "debug prompt-input"):
			return []byte("[]"), nil, nil
		case strings.Contains(joined, "peerctx-read"):
			allowed, err := os.ReadFile(filepath.Join(directory, "allowed.txt"))
			return allowed, nil, err
		case strings.Contains(joined, "peerctx-write"):
			if err := os.WriteFile(filepath.Join(directory, "write-profile-write-probe"), []byte("ok"), 0600); err != nil {
				return nil, nil, err
			}
			gitCommon := args[len(args)-3]
			allowed, err := os.ReadFile(filepath.Join(directory, "allowed.txt"))
			if err != nil {
				return nil, nil, err
			}
			metadata, err := os.ReadFile(filepath.Join(gitCommon, "metadata.txt"))
			if err != nil {
				return nil, nil, err
			}
			return []byte(strings.TrimSpace(string(allowed)) + "|" + strings.TrimSpace(string(metadata))), nil, nil
		default:
			return nil, nil, errors.New("unexpected capability command")
		}
	}
	if err := probeRuntimeCapabilities("/fake/codex", hostAuth, runner); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 5 {
		t.Fatalf("capability commands = %d, want 5: %v", len(calls), calls)
	}
}

func TestRuntimeCapabilityProbeFailsClosedWhenInterfaceChanges(t *testing.T) {
	hostAuth := filepath.Join(t.TempDir(), "auth.json")
	if err := os.WriteFile(hostAuth, []byte("{}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	runner := func(_ context.Context, _ string, _ []string, _ string, args ...string) ([]byte, []byte, error) {
		if strings.Join(args, " ") == "exec --help" {
			return []byte("--strict-config --ephemeral --ignore-rules --skip-git-repo-check"), nil, nil
		}
		return nil, nil, errors.New("should not continue after interface failure")
	}
	err := probeRuntimeCapabilities("/fake/codex", hostAuth, runner)
	var runtimeErr *RuntimeError
	if !errors.As(err, &runtimeErr) || runtimeErr.Code != "codex_interface_unsupported" {
		t.Fatalf("error = %#v", err)
	}
}

func TestRuntimeCapabilityProbeFailsClosedWhenReadProfileCanWrite(t *testing.T) {
	hostAuth := filepath.Join(t.TempDir(), "auth.json")
	if err := os.WriteFile(hostAuth, []byte("{}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	runner := func(_ context.Context, _ string, _ []string, directory string, args ...string) ([]byte, []byte, error) {
		joined := strings.Join(args, " ")
		switch {
		case joined == "exec --help":
			return []byte("--strict-config --ephemeral --ignore-rules --skip-git-repo-check --json"), nil, nil
		case strings.Contains(joined, "login status"), strings.Contains(joined, "debug prompt-input"):
			return []byte("ok"), nil, nil
		case strings.Contains(joined, "peerctx-read"):
			if err := os.WriteFile(filepath.Join(directory, "read-profile-write-probe"), []byte("unsafe"), 0600); err != nil {
				return nil, nil, err
			}
			allowed, err := os.ReadFile(filepath.Join(directory, "allowed.txt"))
			return allowed, nil, err
		default:
			return nil, nil, errors.New("should stop after the read boundary failure")
		}
	}
	err := probeRuntimeCapabilities("/fake/codex", hostAuth, runner)
	var runtimeErr *RuntimeError
	if !errors.As(err, &runtimeErr) || runtimeErr.Code != "codex_read_isolation_probe_failed" {
		t.Fatalf("error = %#v", err)
	}
}
