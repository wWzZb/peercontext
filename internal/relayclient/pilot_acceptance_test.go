package relayclient

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/wWzZb/peercontext/internal/relay"
	protocolv1 "github.com/wWzZb/peercontext/pkg/protocol/v1"
)

func TestTenCrossRepositoryPilotFixturesCloseInOneRequestWithSafetyGuards(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "relay.sqlite")
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	store, err := relay.OpenStore(dbPath, logger)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	server, _ := relay.NewServer(store, logger, relay.WithReadTimeout(2*time.Second), relay.WithWriteTimeout(2*time.Second))
	httpServer := httptest.NewServer(server.Handler())
	t.Cleanup(httpServer.Close)
	provider, requester, agent, member := setupPilotProject(t, store, httpServer.URL)
	providerCtx, stop := context.WithCancel(t.Context())
	ready := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- provider.ServeAgentWithJobHandler(providerCtx, agent.ID, func(map[string]any) { close(ready) }, func(_ context.Context, request protocolv1.Request, commit string) (protocolv1.Response, *protocolv1.RequestFailure) {
			answer := append([]byte("verified:"), request.Body...)
			response := protocolv1.Response{SchemaVersion: 1, RequestID: request.ID, Status: protocolv1.StatusSucceeded, Answer: answer}
			if request.Mode == protocolv1.ModeWrite {
				worktree := protocolv1.WorktreeResult{SchemaVersion: 1, ID: "wt_pilot", AgentID: agent.ID, RequestID: request.ID, BaseCommit: commit}
				response.Worktree = &worktree
			}
			return response, nil
		})
	}()
	<-ready
	t.Cleanup(func() { stop(); <-done })

	cases := []string{
		"api-contract-fields-nullability-errors",
		"shared-model-enum-transitions",
		"authentication-refresh-boundaries",
		"deployment-environment-constraints",
		"internal-sdk-compatible-usage",
		"private-component-composition-limits",
		"cross-repository-failure-root-cause",
		"migration-protocol-compatibility",
		"test-fixture-boundaries",
	}
	passed := 0
	for index, name := range cases {
		body := []byte("PEERCTX_PILOT_" + name)
		now := time.Now().UTC()
		expires := now.Add(2 * time.Second)
		request := protocolv1.Request{SchemaVersion: 1, ID: fmt.Sprintf("req_pilot_%02d", index+1), ProjectID: member.ProjectID, RequesterID: member.ID, AgentID: agent.ID, Mode: protocolv1.ModeRead, Body: body, BodySHA256: protocolv1.BodySHA256(body), CreatedAt: now, ExpiresAt: &expires}
		result, err := requester.Submit(t.Context(), request)
		if err == nil && result.Response != nil && bytes.Equal(result.Response.Answer, append([]byte("verified:"), body...)) {
			passed++
		}
	}

	writeBody := []byte("PEERCTX_PILOT_cross-repository-approved-change")
	write := writeRequest("req_pilot_10", member, agent.ID, writeBody, time.Now().UTC().Add(2*time.Second))
	writeResult := make(chan protocolv1.AskResult, 1)
	writeErr := make(chan error, 1)
	go func() {
		result, err := requester.Submit(t.Context(), write)
		writeResult <- result
		writeErr <- err
	}()
	waitForPending(t, provider, write.ID)
	if _, err := provider.ApproveRequest(t.Context(), write.ID, "0123456789abcdef"); err != nil {
		t.Fatal(err)
	}
	result := <-writeResult
	if err := <-writeErr; err == nil && result.Response != nil && result.Response.Worktree != nil && result.Response.Worktree.BaseCommit == "0123456789abcdef" {
		passed++
	}
	if passed != 10 {
		t.Fatalf("pilot fixtures passed %d/10, safety gate requires at least 8/10", passed)
	}
	canaries := append([]string{}, cases...)
	canaries = append(canaries, string(writeBody), "verified:"+string(writeBody), "/private/provider/pilot-repository")
	assertRelayCanariesAbsent(t, dbPath, logs.String(), canaries...)
}

func setupPilotProject(t *testing.T, store *relay.Store, baseURL string) (*Client, *Client, relay.Agent, relay.Member) {
	t.Helper()
	_, _, ownerToken, err := store.CreateProject(t.Context(), "pilot", "owner")
	if err != nil {
		t.Fatal(err)
	}
	owner, _ := store.Authenticate(t.Context(), ownerToken)
	_, invite, _ := store.CreateInvite(t.Context(), owner, time.Hour)
	_, requester, requesterToken, err := store.JoinInvite(t.Context(), invite, "requester")
	if err != nil {
		t.Fatal(err)
	}
	agent, err := store.RegisterAgent(t.Context(), owner, protocolv1.AgentManifest{SchemaVersion: 1, Name: "pilot-provider", Summary: "Cross-repository pilot fixtures", Modes: []protocolv1.RequestMode{protocolv1.ModeRead, protocolv1.ModeWrite}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.SetACL(t.Context(), owner, agent.ID, requester.ID, []protocolv1.RequestMode{protocolv1.ModeRead, protocolv1.ModeWrite}, true); err != nil {
		t.Fatal(err)
	}
	provider, _ := New(baseURL, ownerToken)
	requesterClient, _ := New(baseURL, requesterToken)
	return provider, requesterClient, agent, requester
}
