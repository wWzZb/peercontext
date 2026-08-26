package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/wWzZb/peercontext/internal/version"
	"github.com/wWzZb/peercontext/pkg/clioutput"
	protocolv1 "github.com/wWzZb/peercontext/pkg/protocol/v1"
)

func TestVersionWritesSuccessOnlyToStdout(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := Run([]string{"version"}, bytes.NewReader(nil), &stdout, &stderr)

	if exitCode != clioutput.ExitOK {
		t.Fatalf("exit code = %d, want %d", exitCode, clioutput.ExitOK)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	var envelope struct {
		OK   bool        `json:"ok"`
		Data VersionData `json:"data"`
		Meta struct {
			Version string `json:"version"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatalf("decode stdout: %v", err)
	}
	if !envelope.OK {
		t.Fatal("ok = false, want true")
	}
	if envelope.Data.SchemaVersion != protocolv1.SchemaVersion {
		t.Fatalf("schema version = %d, want %d", envelope.Data.SchemaVersion, protocolv1.SchemaVersion)
	}
	if envelope.Data.Version != version.Current || envelope.Meta.Version != version.Current {
		t.Fatalf("versions = data %q meta %q, want %q", envelope.Data.Version, envelope.Meta.Version, version.Current)
	}
	if envelope.Data.Protocol != protocolv1.ProtocolVersion {
		t.Fatalf("protocol = %q, want %q", envelope.Data.Protocol, protocolv1.ProtocolVersion)
	}
	if envelope.Data.RuntimeMode != protocolv1.RuntimeModeIsolated {
		t.Fatalf("runtime mode = %q, want %q", envelope.Data.RuntimeMode, protocolv1.RuntimeModeIsolated)
	}
	if stdout.Bytes()[stdout.Len()-1] != '\n' {
		t.Fatal("stdout is not newline terminated")
	}
}

func TestUnknownCommandWritesStructuredErrorOnlyToStderr(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := Run([]string{"does-not-exist"}, bytes.NewReader(nil), &stdout, &stderr)

	if exitCode != clioutput.ExitUsage {
		t.Fatalf("exit code = %d, want %d", exitCode, clioutput.ExitUsage)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	var envelope clioutput.ErrorEnvelope
	if err := json.Unmarshal(stderr.Bytes(), &envelope); err != nil {
		t.Fatalf("decode stderr: %v", err)
	}
	if envelope.OK {
		t.Fatal("ok = true, want false")
	}
	if envelope.Error.Code != "unknown_command" {
		t.Fatalf("error code = %q, want unknown_command", envelope.Error.Code)
	}
	if envelope.Error.Retryable {
		t.Fatal("retryable = true, want false")
	}
}

func TestRelayConnectionFailureUsesTransportExitCode(t *testing.T) {
	t.Setenv("PEERCTX_CONFIG_DIR", t.TempDir())
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := Run([]string{"project", "create", "--relay", "http://127.0.0.1:1", "--name", "test", "--owner", "alice", "--credential-file", t.TempDir() + "/credential"}, bytes.NewReader(nil), &stdout, &stderr)
	if exitCode != clioutput.ExitConnection {
		t.Fatalf("exit code = %d, want %d; stderr=%s", exitCode, clioutput.ExitConnection, stderr.String())
	}
	var envelope clioutput.ErrorEnvelope
	if err := json.Unmarshal(stderr.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Error.Code != "relay_connection_failed" || !envelope.Error.Retryable {
		t.Fatalf("error = %#v", envelope.Error)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestSkillsListAndReadExposeVersionMatchedExplicitBundle(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"skills", "list"}, strings.NewReader(""), &stdout, &stderr); code != clioutput.ExitOK {
		t.Fatalf("skills list code=%d stderr=%s", code, stderr.String())
	}
	var list struct {
		Data struct {
			Skills []struct {
				Name     string   `json:"name"`
				Version  string   `json:"version"`
				Files    []string `json:"files"`
				Implicit bool     `json:"implicit_invocation"`
			} `json:"skills"`
		} `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Data.Skills) != 1 || list.Data.Skills[0].Name != "peer-context" || list.Data.Skills[0].Version != version.Current || list.Data.Skills[0].Implicit || len(list.Data.Skills[0].Files) != 5 {
		t.Fatalf("skills list = %#v", list)
	}
	stdout.Reset()
	if code := Run([]string{"skills", "read", "peer-context", "--file", "agents/openai.yaml"}, strings.NewReader(""), &stdout, &stderr); code != clioutput.ExitOK {
		t.Fatalf("skills read code=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "allow_implicit_invocation: false") {
		t.Fatalf("skills read = %s", stdout.String())
	}
}
