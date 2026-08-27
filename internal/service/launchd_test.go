package service

import (
	"strings"
	"testing"

	"github.com/wWzZb/peercontext/internal/v2state"
)

func TestLaunchAgentPlistKeepsServiceAliveWithoutNetworkConfiguration(t *testing.T) {
	t.Setenv("PATH", "/usr/bin:/opt/homebrew/bin")
	manager, _ := v2state.NewManager(t.TempDir())
	agent := &LaunchAgent{State: manager, BinaryPath: "/Applications/peerctx", UID: 501}
	plist := string(agent.plist())
	for _, required := range []string{LaunchAgentLabel, "_service-run", "RunAtLoad", "KeepAlive", "/usr/bin:/opt/homebrew/bin"} {
		if !strings.Contains(plist, required) {
			t.Fatalf("plist missing %q", required)
		}
	}
	for _, forbidden := range []string{"relay", "Bearer", "--listen", "--port"} {
		if strings.Contains(plist, forbidden) {
			t.Fatalf("plist contains user-facing infrastructure %q", forbidden)
		}
	}
}
