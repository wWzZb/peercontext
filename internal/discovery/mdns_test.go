package discovery

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	protocolv2 "github.com/wWzZb/peercontext/pkg/protocol/v2"
)

func TestDiscoveryRejectsForgedBroadcastIdentity(t *testing.T) {
	hostPublic, hostPrivate, _ := ed25519.GenerateKey(rand.Reader)
	_, forgedPrivate, _ := ed25519.GenerateKey(rand.Reader)
	makeServer := func(privateKey ed25519.PrivateKey) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			payload, _ := json.Marshal(map[string]any{"ok": true})
			message := protocolv2.NewSignedReply("project", "host", "identity.response", http.MethodGet, "/v2/identity", r.URL.Query().Get("challenge"), "nonce-"+time.Now().String(), payload, time.Now().UTC(), privateKey)
			_ = json.NewEncoder(w).Encode(message)
		}))
	}
	valid := makeServer(hostPrivate)
	defer valid.Close()
	forged := makeServer(forgedPrivate)
	defer forged.Close()
	result := validateCandidates(t.Context(), []string{forged.URL, valid.URL}, "project", hostPublic)
	if len(result) != 1 || result[0] != valid.URL {
		t.Fatalf("validated endpoints = %#v", result)
	}
}
