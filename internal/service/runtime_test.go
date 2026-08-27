package service

import (
	"errors"
	"strings"
	"testing"

	"github.com/wWzZb/peercontext/internal/codex"
)

func TestRuntimeFailureDoesNotExposeRepositoryPath(t *testing.T) {
	secretPath := "/Users/provider/secret-repository"
	err := &codex.RuntimeError{Code: "agent_repository_unavailable", Message: "The Agent repository is unavailable", Err: errors.New("stat " + secretPath)}
	code, message, _ := publicRuntimeFailure(err)
	if code != "agent_repository_unavailable" || strings.Contains(message, secretPath) {
		t.Fatalf("public failure code=%q message=%q", code, message)
	}
}
