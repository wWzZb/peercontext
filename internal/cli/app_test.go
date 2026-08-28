package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"

	"github.com/wWzZb/peercontext/pkg/clioutput"
)

func TestVersionReportsLANV2(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"version"}, bytes.NewReader(nil), &stdout, &stderr); code != clioutput.ExitOK {
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
