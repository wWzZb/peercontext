package clioutput

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestWriteSuccessContract(t *testing.T) {
	var output bytes.Buffer
	data := struct {
		SchemaVersion int `json:"schema_version"`
	}{SchemaVersion: 1}

	if err := WriteSuccess(&output, data, Meta{RequestID: "req_123", Version: "0.1.0"}); err != nil {
		t.Fatalf("WriteSuccess: %v", err)
	}

	want := "{\"ok\":true,\"data\":{\"schema_version\":1},\"meta\":{\"request_id\":\"req_123\",\"version\":\"0.1.0\"}}\n"
	if output.String() != want {
		t.Fatalf("output = %q, want %q", output.String(), want)
	}
}

func TestWriteErrorContractAndExitCode(t *testing.T) {
	apiErr := NewError(
		ExitConfirmationRequired,
		"approval",
		"requester_confirmation",
		"write_confirmation_required",
		"The write request requires confirmation.",
		"Ask the requester to confirm this exact request.",
		false,
	)
	var output bytes.Buffer

	if err := WriteError(&output, apiErr); err != nil {
		t.Fatalf("WriteError: %v", err)
	}
	if apiErr.ExitCode() != 10 {
		t.Fatalf("exit code = %d, want 10", apiErr.ExitCode())
	}
	if strings.Contains(output.String(), "exitCode") || strings.Contains(output.String(), "exit_code") {
		t.Fatalf("private process exit code leaked into JSON: %s", output.String())
	}

	var envelope ErrorEnvelope
	if err := json.Unmarshal(output.Bytes(), &envelope); err != nil {
		t.Fatalf("decode output: %v", err)
	}
	if envelope.OK || envelope.Error.Code != "write_confirmation_required" {
		t.Fatalf("unexpected envelope: %#v", envelope)
	}
}

func TestZeroErrorExitCodeFallsBackToInternal(t *testing.T) {
	apiErr := NewError(ExitOK, "internal", "unexpected", "unexpected_error", "unexpected", "retry", true)
	if apiErr.ExitCode() != ExitInternal {
		t.Fatalf("exit code = %d, want %d", apiErr.ExitCode(), ExitInternal)
	}
}

func TestExitCodesAreStableAndUnique(t *testing.T) {
	codes := []ExitCode{
		ExitOK, ExitInternal, ExitUsage, ExitConfiguration, ExitAuthentication,
		ExitAuthorization, ExitConnection, ExitNotFound, ExitConflict,
		ExitUnavailable, ExitConfirmationRequired, ExitDenied, ExitTimeout,
		ExitCanceled, ExitProtocol, ExitRuntime,
	}
	seen := make(map[ExitCode]struct{}, len(codes))
	for want, code := range codes {
		if code != ExitCode(want) {
			t.Fatalf("exit code at position %d = %d", want, code)
		}
		if _, exists := seen[code]; exists {
			t.Fatalf("duplicate exit code %d", code)
		}
		seen[code] = struct{}{}
	}
}

func TestWriterErrorsAreReturned(t *testing.T) {
	err := WriteSuccess(failingWriter{}, struct{}{}, Meta{Version: "0.1.0"})
	if err == nil {
		t.Fatal("WriteSuccess error = nil, want writer error")
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) {
	return 0, bytes.ErrTooLarge
}
