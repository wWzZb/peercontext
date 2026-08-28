package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"testing"

	"github.com/wWzZb/peercontext/pkg/clioutput"
)

func TestVersionReportsLANV2(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"version", "--json"}, bytes.NewReader(nil), &stdout, &stderr); code != clioutput.ExitOK {
		t.Fatalf("exit=%d stderr=%s", code, stderr.String())
	}
	var envelope map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	data := envelope["data"].(map[string]any)
	if data["version"] != "0.2.0-alpha.1" || data["protocol_version"] != "v2" || data["schema_version"] != float64(2) {
		t.Fatalf("version data = %#v", data)
	}
}

func TestDefaultVersionIsHumanReadable(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"version"}, bytes.NewReader(nil), &stdout, &stderr); code != clioutput.ExitOK {
		t.Fatalf("exit=%d stderr=%s", code, stderr.String())
	}
	if !bytes.Contains(stdout.Bytes(), []byte("peerctx 0.2.0-alpha.1")) || bytes.HasPrefix(stdout.Bytes(), []byte("{")) {
		t.Fatalf("human version output = %q", stdout.String())
	}
}

func TestHelpAtEveryLevelDoesNotCreateState(t *testing.T) {
	config := t.TempDir() + "/unused"
	t.Setenv("PEERCTX_CONFIG_DIR", config)
	for _, args := range [][]string{
		{"--help"}, {"help"}, {"project", "--help"}, {"project", "create", "--help"}, {"project", "join", "--help"}, {"project", "list", "--help"}, {"project", "use", "--help"}, {"project", "invite", "create", "--help"}, {"project", "member", "list", "--help"}, {"project", "member", "remove", "--help"},
		{"agent", "--help"}, {"agent", "register", "--help"}, {"agent", "list", "--help"}, {"agent", "get", "--help"}, {"agent", "remove", "--help"}, {"ask", "--help"},
		{"service", "--help"}, {"service", "start", "--help"}, {"service", "stop", "--help"}, {"service", "restart", "--help"}, {"service", "status", "--help"},
		{"skills", "--help"}, {"skills", "list", "--help"}, {"skills", "read", "--help"}, {"version", "--help"},
	} {
		var stdout, stderr bytes.Buffer
		if code := Run(args, errorReader{}, &stdout, &stderr); code != clioutput.ExitOK {
			t.Fatalf("%v exit=%d stderr=%s", args, code, stderr.String())
		}
		if !bytes.Contains(stdout.Bytes(), []byte("Usage:")) || stderr.Len() != 0 {
			t.Fatalf("%v stdout=%q stderr=%q", args, stdout.String(), stderr.String())
		}
	}
	if _, err := os.Stat(config); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("help created state at %s", config)
	}
}

func TestErrorsFollowSelectedOutputMode(t *testing.T) {
	for _, test := range []struct {
		args     []string
		jsonMode bool
	}{
		{args: []string{"unknown"}},
		{args: []string{"unknown", "--json"}, jsonMode: true},
	} {
		var stdout, stderr bytes.Buffer
		if code := Run(test.args, bytes.NewReader(nil), &stdout, &stderr); code != clioutput.ExitUsage {
			t.Fatalf("%v exit=%d", test.args, code)
		}
		if test.jsonMode != bytes.HasPrefix(stderr.Bytes(), []byte("{")) {
			t.Fatalf("%v stderr=%q", test.args, stderr.String())
		}
		if stdout.Len() != 0 {
			t.Fatalf("%v wrote failure to stdout", test.args)
		}
	}
}

func TestRemovedV1CommandsAreUnknown(t *testing.T) {
	for _, args := range [][]string{{"relay", "serve"}, {"task", "agent"}, {"worktree", "list"}, {"credential", "status"}} {
		var stdout, stderr bytes.Buffer
		if code := Run(args, bytes.NewReader(nil), &stdout, &stderr); code != clioutput.ExitUsage {
			t.Fatalf("%v exit=%d stdout=%s stderr=%s", args, code, stdout.String(), stderr.String())
		}
	}
}

func TestRecursiveAskStopsBeforeReadingStdin(t *testing.T) {
	t.Setenv("PEERCTX_INBOUND_REQUEST", "1")
	var stdout, stderr bytes.Buffer
	code := Run([]string{"ask", "agent"}, errorReader{}, &stdout, &stderr)
	if code != clioutput.ExitAuthorization || !bytes.Contains(stderr.Bytes(), []byte("recursive_request_blocked")) {
		t.Fatalf("exit=%d stderr=%s", code, stderr.String())
	}
}

type errorReader struct{}

func (errorReader) Read([]byte) (int, error) { return 0, errors.New("stdin must not be read") }
