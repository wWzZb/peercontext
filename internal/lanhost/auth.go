package lanhost

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"net"
	"strings"
	"sync"
	"time"

	protocolv2 "github.com/wWzZb/peercontext/pkg/protocol/v2"
)

type replayGuard struct {
	mu     sync.Mutex
	seen   map[string]time.Time
	maxAge time.Duration
}

func newReplayGuard(maxAge time.Duration) *replayGuard {
	return &replayGuard{seen: map[string]time.Time{}, maxAge: maxAge}
}

func (g *replayGuard) Accept(message protocolv2.SignedMessage, now time.Time) error {
	key := message.ProjectID + "\x00" + message.SenderID + "\x00" + message.Nonce
	g.mu.Lock()
	defer g.mu.Unlock()
	for candidate, seenAt := range g.seen {
		if now.Sub(seenAt) > g.maxAge*2 {
			delete(g.seen, candidate)
		}
	}
	if _, exists := g.seen[key]; exists {
		return errors.New("signed message nonce was already used")
	}
	g.seen[key] = now
	return nil
}

func newNonce() (string, error) {
	value := make([]byte, 18)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}

func isDirectLANRemote(remoteAddress string) bool {
	host, _, err := net.SplitHostPort(remoteAddress)
	if err != nil {
		return false
	}
	remote := net.ParseIP(strings.Trim(host, "[]"))
	if remote == nil {
		return false
	}
	if remote.IsLoopback() {
		return true
	}
	interfaces, err := net.Interfaces()
	if err != nil {
		return false
	}
	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 || isTunnelInterface(iface.Name) {
			continue
		}
		addresses, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, address := range addresses {
			_, subnet, err := net.ParseCIDR(address.String())
			if err == nil && subnet.Contains(remote) {
				return true
			}
		}
	}
	return false
}

func isTunnelInterface(name string) bool {
	name = strings.ToLower(name)
	for _, prefix := range []string{"utun", "tun", "tap", "ppp", "ipsec", "awdl", "llw"} {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

func publicKey(value []byte) (ed25519.PublicKey, error) {
	if len(value) != ed25519.PublicKeySize {
		return nil, errors.New("invalid Ed25519 public key")
	}
	return ed25519.PublicKey(value), nil
}
