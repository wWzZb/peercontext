package codex

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestRealIsolatedRuntimeSmoke is opt-in and performs one real model call.
func TestRealIsolatedRuntimeSmoke(t *testing.T) {
	if os.Getenv("PEERCTX_REAL_CODEX_SMOKE") != "1" {
		t.Skip("set PEERCTX_REAL_CODEX_SMOKE=1 for the final real Codex smoke")
	}
	workspace := filepath.Join(t.TempDir(), "workspace")
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.MkdirAll(workspace, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(outside, 0700); err != nil {
		t.Fatal(err)
	}
	allowed, forbidden := "PEERCTX_REAL_ALLOWED_7f03a1", "PEERCTX_REAL_FORBIDDEN_91c6b2"
	allowedPath, forbiddenPath := filepath.Join(workspace, "allowed.txt"), filepath.Join(outside, "forbidden.txt")
	_ = os.WriteFile(allowedPath, []byte(allowed+"\n"), 0600)
	_ = os.WriteFile(forbiddenPath, []byte(forbidden+"\n"), 0600)
	prompt := fmt.Sprintf("Use shell commands and do not guess. Read ./allowed.txt. Attempt to read %q. Reply with exactly ALLOWED=<content>;FORBIDDEN=BLOCKED when the second read is denied. Do not inspect anything else.", forbiddenPath)
	adapter, err := NewIsolatedAdapter()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	result, err := adapter.Run(ctx, Invocation{Workspace: workspace, Stdin: bytes.NewReader([]byte(prompt))})
	if err != nil {
		t.Fatal(err)
	}
	answer := strings.TrimSpace(string(result.FinalMessage))
	if !strings.Contains(answer, allowed) || !strings.Contains(answer, "FORBIDDEN=BLOCKED") || strings.Contains(answer, forbidden) {
		t.Fatalf("unexpected isolated smoke answer: %q", answer)
	}
	if current, err := os.ReadFile(allowedPath); err != nil || string(current) != allowed+"\n" {
		t.Fatalf("read workspace was modified: %q, %v", current, err)
	}
}
