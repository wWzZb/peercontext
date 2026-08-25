package v1

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestRequestValidatesRawBodyHash(t *testing.T) {
	body := []byte{'a', '\r', '\n', 0, 0xff, 'z'}
	now := time.Date(2026, 8, 25, 8, 0, 0, 0, time.UTC)
	request := validRequest(body, now)

	if err := request.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if request.BodySHA256 != BodySHA256(body) {
		t.Fatal("hash was not computed from the exact body bytes")
	}
}

func TestRequestRejectsChangedBody(t *testing.T) {
	now := time.Date(2026, 8, 25, 8, 0, 0, 0, time.UTC)
	request := validRequest([]byte("line one\r\nline two"), now)
	request.Body = bytes.ReplaceAll(request.Body, []byte("\r\n"), []byte("\n"))

	err := request.Validate()
	if err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("Validate error = %v, want hash mismatch", err)
	}
}

func TestRequestRejectsOversizeBody(t *testing.T) {
	now := time.Date(2026, 8, 25, 8, 0, 0, 0, time.UTC)
	request := validRequest(make([]byte, MaxRequestBodyBytes+1), now)

	err := request.Validate()
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("Validate error = %v, want size error", err)
	}
}

func TestRequestJSONBase64EncodesRawBodyBytes(t *testing.T) {
	body := []byte{'a', '\r', '\n', 0, 0xff, 'z'}
	request := validRequest(body, time.Date(2026, 8, 25, 8, 0, 0, 0, time.UTC))

	encoded, err := json.Marshal(request)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	var fields map[string]any
	if err := json.Unmarshal(encoded, &fields); err != nil {
		t.Fatalf("decode request: %v", err)
	}
	want := base64.StdEncoding.EncodeToString(body)
	if fields["body"] != want {
		t.Fatalf("body = %v, want base64 %q", fields["body"], want)
	}

	var decoded Request
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("unmarshal request: %v", err)
	}
	if !bytes.Equal(decoded.Body, body) {
		t.Fatalf("decoded body = %v, want %v", decoded.Body, body)
	}
}

func TestRequestMetadataContainsNoBody(t *testing.T) {
	canary := []byte("peerctx-secret-body-canary")
	now := time.Date(2026, 8, 25, 8, 0, 0, 0, time.UTC)
	request := validRequest(canary, now)

	encoded, err := json.Marshal(request.Metadata(StatusRunning, now.Add(time.Second)))
	if err != nil {
		t.Fatalf("marshal metadata: %v", err)
	}
	if bytes.Contains(encoded, canary) {
		t.Fatalf("metadata contains request body: %s", encoded)
	}
	var fields map[string]any
	if err := json.Unmarshal(encoded, &fields); err != nil {
		t.Fatalf("decode metadata: %v", err)
	}
	if _, exists := fields["body"]; exists {
		t.Fatalf("metadata has a body field: %s", encoded)
	}
	if fields["body_sha256"] != BodySHA256(canary) {
		t.Fatalf("body hash = %v, want %s", fields["body_sha256"], BodySHA256(canary))
	}
	if err := request.Metadata(StatusRunning, now.Add(time.Second)).Validate(); err != nil {
		t.Fatalf("Validate metadata: %v", err)
	}
}

func TestAgentManifestOnlyDoesStructuralValidation(t *testing.T) {
	manifest := AgentManifest{
		SchemaVersion: SchemaVersion,
		Name:          "backend-bob",
		Summary:       "订单与支付后端仓库",
		Tags:          []string{"backend", "orders"},
		Capabilities:  []string{"API contract", "approved code changes"},
		Modes:         []RequestMode{ModeRead, ModeWrite},
	}

	if err := manifest.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	encoded, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	if bytes.Contains(encoded, []byte("repository")) || bytes.Contains(encoded, []byte("/Users/")) {
		t.Fatalf("manifest exposes provider-local repository data: %s", encoded)
	}
}

func TestAgentManifestRejectsDuplicateOrUnknownModes(t *testing.T) {
	tests := []struct {
		name  string
		modes []RequestMode
	}{
		{name: "duplicate", modes: []RequestMode{ModeRead, ModeRead}},
		{name: "unknown", modes: []RequestMode{"full"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			manifest := AgentManifest{
				SchemaVersion: SchemaVersion,
				Name:          "backend-bob",
				Summary:       "backend",
				Modes:         test.modes,
			}
			if err := manifest.Validate(); err == nil {
				t.Fatal("Validate error = nil, want structural error")
			}
		})
	}
}

func TestOnlyIsolatedRuntimeModeIsDefined(t *testing.T) {
	if RuntimeModeIsolated != "isolated_runtime" {
		t.Fatalf("runtime mode = %q, want isolated_runtime", RuntimeModeIsolated)
	}
}

func TestWriteConfirmationBindsAgentModeHashAndExpiryWithoutBody(t *testing.T) {
	now := time.Date(2026, 8, 25, 8, 0, 0, 0, time.UTC)
	body := []byte("change exactly this")
	confirmation := WriteConfirmation{
		SchemaVersion: SchemaVersion,
		AgentID:       "agent_123",
		Mode:          ModeWrite,
		BodyBytes:     len(body),
		BodySHA256:    BodySHA256(body),
		ExpiresAt:     now.Add(10 * time.Minute),
	}
	if err := confirmation.Validate(now); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	encoded, err := json.Marshal(confirmation)
	if err != nil {
		t.Fatalf("marshal confirmation: %v", err)
	}
	if bytes.Contains(encoded, body) || bytes.Contains(encoded, []byte(`"body"`)) {
		t.Fatalf("confirmation contains body: %s", encoded)
	}

	confirmation.Mode = ModeRead
	if err := confirmation.Validate(now); err == nil {
		t.Fatal("read-mode confirmation was accepted")
	}
	confirmation.Mode = ModeWrite
	confirmation.ExpiresAt = now
	if err := confirmation.Validate(now); err == nil {
		t.Fatal("expired confirmation was accepted")
	}
}

func TestResponseCannotCarryInfrastructureFailureAsAnswer(t *testing.T) {
	response := Response{
		SchemaVersion: SchemaVersion,
		RequestID:     "req_123",
		Status:        StatusFailed,
		Answer:        []byte("network failed"),
	}
	if err := response.Validate(); err == nil {
		t.Fatal("failed request was accepted as an answer response")
	}
}

func TestSucceededResponseRequiresFinalAnswer(t *testing.T) {
	response := Response{
		SchemaVersion: SchemaVersion,
		RequestID:     "req_123",
		Status:        StatusSucceeded,
	}
	if err := response.Validate(); err == nil || !strings.Contains(err.Error(), "answer is required") {
		t.Fatalf("Validate error = %v, want missing answer error", err)
	}

	response.Answer = []byte{}
	if err := response.Validate(); err != nil {
		t.Fatalf("explicit empty final answer should remain representable: %v", err)
	}
}

func validRequest(body []byte, now time.Time) Request {
	expires := now.Add(10 * time.Minute)
	return Request{
		SchemaVersion: SchemaVersion,
		ID:            "req_123",
		ProjectID:     "project_123",
		RequesterID:   "member_123",
		AgentID:       "agent_123",
		Mode:          ModeRead,
		Body:          append([]byte(nil), body...),
		BodySHA256:    BodySHA256(body),
		CreatedAt:     now,
		ExpiresAt:     &expires,
	}
}
