package cli

import (
	"bytes"
	"encoding/json"
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
