package lanhost

import (
	"crypto/ed25519"
	"crypto/rand"
	"testing"
	"time"

	protocolv2 "github.com/wWzZb/peercontext/pkg/protocol/v2"
)

func TestReplayGuardRejectsNonceReuse(t *testing.T) {
	_, privateKey, _ := ed25519.GenerateKey(rand.Reader)
	now := time.Now().UTC()
	message := protocolv2.NewSignedMessage("p", "m", "agents.list", "one-nonce", nil, now, privateKey)
	guard := newReplayGuard(time.Minute)
	if err := guard.Accept(message, now); err != nil {
		t.Fatal(err)
	}
	if err := guard.Accept(message, now); err == nil {
		t.Fatal("replayed nonce was accepted")
	}
}

func TestDirectLANCheckRejectsPublicInternetAddress(t *testing.T) {
	if isDirectLANRemote("8.8.8.8:443") {
		t.Fatal("public Internet address was treated as a directly connected LAN peer")
	}
	if !isDirectLANRemote("127.0.0.1:1234") {
		t.Fatal("loopback must be accepted for local control and integration tests")
	}
}
