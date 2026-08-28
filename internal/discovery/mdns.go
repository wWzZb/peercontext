package discovery

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/hashicorp/mdns"
	"github.com/wWzZb/peercontext/internal/failure"
	protocolv2 "github.com/wWzZb/peercontext/pkg/protocol/v2"
)

const Service = "_peerctx._tcp"

type Advertiser struct {
	server *mdns.Server
}

func Start(instance string, port int) (*Advertiser, error) {
	service, err := mdns.NewMDNSService(instance, Service, "", "", port, nil, []string{"v=2"})
	if err != nil {
		return nil, err
	}
	server, err := mdns.NewServer(&mdns.Config{Zone: service})
	if err != nil {
		return nil, err
	}
	return &Advertiser{server: server}, nil
}

func (a *Advertiser) Close() error {
	if a == nil || a.server == nil {
		return nil
	}
	return a.server.Shutdown()
}

func Discover(ctx context.Context, projectID string, hostPublicKey ed25519.PublicKey) ([]string, error) {
	entries := make(chan *mdns.ServiceEntry, 32)
	queryDone := make(chan error, 1)
	go func() {
		queryDone <- mdns.Query(&mdns.QueryParam{Service: Service, Domain: "local", Timeout: 2 * time.Second, Entries: entries, DisableIPv6: true})
		close(entries)
	}()
	seen := map[string]struct{}{}
	var candidates []string
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case entry, ok := <-entries:
			if !ok {
				entries = nil
				continue
			}
			if entry == nil || entry.AddrV4 == nil || entry.Port <= 0 {
				continue
			}
			endpoint := "http://" + net.JoinHostPort(entry.AddrV4.String(), strconv.Itoa(entry.Port))
			if _, exists := seen[endpoint]; !exists {
				seen[endpoint] = struct{}{}
				candidates = append(candidates, endpoint)
			}
		case err := <-queryDone:
			if err != nil && len(candidates) == 0 {
				return nil, failure.Wrap("lan_discovery_unavailable", "LAN discovery is unavailable on this network.", true, err)
			}
			valid := validateCandidates(ctx, candidates, projectID, hostPublicKey)
			if len(valid) == 0 {
				return nil, failure.New("project_host_offline", "The Project host is offline or was not found on this LAN.", true)
			}
			return valid, nil
		}
	}
}

func validateCandidates(ctx context.Context, candidates []string, projectID string, hostPublicKey ed25519.PublicKey) []string {
	client := &http.Client{Timeout: 1500 * time.Millisecond}
	var valid []string
	for _, endpoint := range candidates {
		rawChallenge := make([]byte, 18)
		if _, err := rand.Read(rawChallenge); err != nil {
			continue
		}
		challenge := base64.RawURLEncoding.EncodeToString(rawChallenge)
		identityURL := strings.TrimRight(endpoint, "/") + "/v2/identity?project_id=" + url.QueryEscape(projectID) + "&challenge=" + url.QueryEscape(challenge)
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, identityURL, nil)
		if err != nil {
			continue
		}
		response, err := client.Do(request)
		if err != nil {
			continue
		}
		var message protocolv2.SignedMessage
		err = json.NewDecoder(response.Body).Decode(&message)
		_ = response.Body.Close()
		if err != nil || message.ProjectID != projectID || message.Kind != "identity.response" || message.Method != http.MethodGet || message.Path != "/v2/identity" || message.ReplyTo != challenge {
			continue
		}
		if message.Verify(hostPublicKey, time.Now().UTC(), protocolv2.DefaultSignatureMaxAge) == nil {
			valid = append(valid, endpoint)
		}
	}
	sort.Strings(valid)
	return valid
}
