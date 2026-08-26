package relay

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	protocolv1 "github.com/wWzZb/peercontext/pkg/protocol/v1"
)

func TestHTTPWebSocketPresenceAndSafeLogs(t *testing.T) {
	store := openTestStore(t)
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	server, err := NewServer(store, logger)
	if err != nil {
		t.Fatal(err)
	}
	httpServer := httptest.NewServer(server.Handler())
	t.Cleanup(httpServer.Close)

	const authCanary = "PEERCTX_AUTHORIZATION_CANARY_94f54e2f"
	project, owner, token, err := store.CreateProject(t.Context(), "backend", "alice")
	if err != nil {
		t.Fatal(err)
	}
	_ = project
	principal, _ := store.Authenticate(t.Context(), token)
	manifest := protocolv1.AgentManifest{SchemaVersion: 1, Name: "backend", Summary: "Backend", Modes: []protocolv1.RequestMode{protocolv1.ModeRead}}
	agent, err := store.RegisterAgent(t.Context(), principal, manifest)
	if err != nil {
		t.Fatal(err)
	}

	wsURL := "ws" + strings.TrimPrefix(httpServer.URL, "http") + "/v1/agents/" + agent.ID + "/serve"
	headers := http.Header{"Authorization": []string{"Bearer " + token}}
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, headers)
	if err != nil {
		t.Fatalf("Dial serve websocket: %v", err)
	}
	var ready map[string]any
	if err := conn.ReadJSON(&ready); err != nil {
		t.Fatalf("read ready: %v", err)
	}
	if ready["runtime_mode"] != string(protocolv1.RuntimeModeIsolated) {
		t.Fatalf("ready = %#v", ready)
	}

	got := getAgentHTTP(t, httpServer.URL, token, agent.ID)
	if !got.Online {
		t.Fatal("agent is offline while serve websocket is connected")
	}
	if err := conn.WriteJSON(map[string]any{"type": "ping"}); err != nil {
		t.Fatal(err)
	}
	var pong map[string]any
	if err := conn.ReadJSON(&pong); err != nil || pong["type"] != "pong" {
		t.Fatalf("pong = %#v, %v", pong, err)
	}
	_ = conn.Close()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if !getAgentHTTP(t, httpServer.URL, token, agent.ID).Online {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if getAgentHTTP(t, httpServer.URL, token, agent.ID).Online {
		t.Fatal("agent stayed online after websocket disconnect")
	}

	req, _ := http.NewRequest(http.MethodGet, httpServer.URL+"/v1/credential/status", nil)
	req.Header.Set("Authorization", "Bearer "+authCanary)
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if strings.Contains(logs.String(), authCanary) || strings.Contains(logs.String(), token) {
		t.Fatalf("Relay log contains credential: %s", logs.String())
	}
	if strings.Contains(logs.String(), agent.ID) {
		t.Fatalf("Relay log contains URL values: %s", logs.String())
	}
	if owner.ID == "" {
		t.Fatal("owner not created")
	}
}

func getAgentHTTP(t *testing.T, baseURL, token, agentID string) Agent {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/v1/agents/%s", baseURL, agentID), nil)
	req.Header.Set("Authorization", "Bearer "+token)
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("GET agent status = %d", response.StatusCode)
	}
	var agent Agent
	if err := json.NewDecoder(response.Body).Decode(&agent); err != nil {
		t.Fatal(err)
	}
	return agent
}

func TestValidateTLSRequirement(t *testing.T) {
	for _, address := range []string{"127.0.0.1:8080", "[::1]:8080", "localhost:8080"} {
		if err := ValidateTLSRequirement(address, "", ""); err != nil {
			t.Fatalf("loopback %s: %v", address, err)
		}
	}
	if err := ValidateTLSRequirement("0.0.0.0:8080", "", ""); err == nil {
		t.Fatal("non-loopback listener accepted without TLS")
	}
	if err := ValidateTLSRequirement("0.0.0.0:8080", "cert.pem", "key.pem"); err != nil {
		t.Fatalf("non-loopback TLS: %v", err)
	}
}
