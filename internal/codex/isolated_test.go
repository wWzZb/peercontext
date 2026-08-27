package codex

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"testing"
)

func TestParseFinalAgentMessageReturnsExactLastMessageBytes(t *testing.T) {
	jsonl := "{\"type\":\"thread.started\"}\n{\"type\":\"item.completed\",\"item\":{\"type\":\"agent_message\",\"text\":\"first\"}}\n{\"type\":\"item.completed\",\"item\":{\"type\":\"agent_message\",\"text\":\"line 1\\r\\nline 2\"}}\n{\"type\":\"turn.completed\"}\n"
	final, err := parseFinalAgentMessage(strings.NewReader(jsonl), 1024)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(final, []byte("line 1\r\nline 2")) {
		t.Fatalf("final = %q", final)
	}
}

func TestParseFinalAgentMessageRejectsMissingMalformedAndOversized(t *testing.T) {
	for name, input := range map[string]string{"missing": "{\"type\":\"turn.completed\"}\n", "malformed": "not-json\n", "oversized": "{\"type\":\"item.completed\",\"item\":{\"type\":\"agent_message\",\"text\":\"12345\"}}\n"} {
		t.Run(name, func(t *testing.T) {
			limit := 1024
			if name == "oversized" {
				limit = 4
			}
			_, err := parseFinalAgentMessage(strings.NewReader(input), limit)
			var runtimeErr *RuntimeError
			if !errors.As(err, &runtimeErr) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestIsolatedReadConfigHasNoWriteOrHostFallback(t *testing.T) {
	t.Setenv("HTTP_PROXY", "http://proxy-user:PEERCTX_PROXY_SECRET@proxy.invalid")
	config := isolatedReadConfigFor("/clean/home", "/clean/tmp")
	for _, required := range []string{"default_permissions = \"peerctx-read\"", "\":root\" = \"deny\"", "\".\" = \"read\"", "enabled = false", "inherit = \"none\"", "PEERCTX_INBOUND_REQUEST"} {
		if !strings.Contains(config, required) {
			t.Fatalf("config missing %q", required)
		}
	}
	for _, forbidden := range []string{"danger-full-access", "unrestricted", "workspace-write", "peerctx-write", "PEERCTX_PROXY_SECRET"} {
		if strings.Contains(config, forbidden) {
			t.Fatalf("config contains forbidden value %q", forbidden)
		}
	}
	t.Setenv("PEERCTX_HOST_SECRET_CANARY", "must-not-pass")
	env := strings.Join(isolatedEnvironment("/clean/home", "/clean/codex", "/clean/tmp"), "\n")
	if strings.Contains(env, "PEERCTX_HOST_SECRET_CANARY") || strings.Contains(env, "must-not-pass") {
		t.Fatal("host environment canary entered isolated runtime")
	}
	if !strings.Contains(env, "PEERCTX_INBOUND_REQUEST=1") {
		t.Fatal("recursive request marker missing")
	}
	if err := ValidateIsolationPolicy(); err != nil {
		t.Fatal(err)
	}
	_ = os.DevNull
}
