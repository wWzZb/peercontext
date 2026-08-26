package relayclient

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wWzZb/peercontext/internal/relay"
	protocolv1 "github.com/wWzZb/peercontext/pkg/protocol/v1"
)

func TestWriteRequiresProviderApprovalAndNeverPersistsBodies(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "relay.sqlite")
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	store, err := relay.OpenStore(dbPath, logger)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	server, _ := relay.NewServer(store, logger, relay.WithWriteTimeout(2*time.Second))
	httpServer := httptest.NewServer(server.Handler())
	t.Cleanup(httpServer.Close)
	provider, requester, agent, member := setupWriteProject(t, store, httpServer.URL)

	const bodyCanary = "PEERCTX_WRITE_BODY_CANARY_7e34a9"
	const answerCanary = "PEERCTX_WRITE_ANSWER_CANARY_30bc2e"
	const pathCanary = "/private/provider/PEERCTX_WRITE_PATH_CANARY_0f42"
	var invocations atomic.Int32
	jobs := make(chan struct {
		body   []byte
		commit string
	}, 4)
	providerCtx, stop := context.WithCancel(t.Context())
	ready := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- provider.ServeAgentWithJobHandler(providerCtx, agent.ID, func(map[string]any) { close(ready) }, func(_ context.Context, request protocolv1.Request, baseCommit string) (protocolv1.Response, *protocolv1.RequestFailure) {
			invocations.Add(1)
			jobs <- struct {
				body   []byte
				commit string
			}{append([]byte(nil), request.Body...), baseCommit}
			worktree := protocolv1.WorktreeResult{SchemaVersion: 1, ID: "wt_safe", AgentID: agent.ID, RequestID: request.ID, BaseCommit: baseCommit}
			return protocolv1.Response{SchemaVersion: 1, RequestID: request.ID, Status: protocolv1.StatusSucceeded, Answer: []byte(answerCanary), Worktree: &worktree}, nil
		})
	}()
	<-ready
	t.Cleanup(func() {
		stop()
		select {
		case <-done:
		case <-time.After(time.Second):
		}
	})

	request := writeRequest("req_write_approve", member, agent.ID, []byte(bodyCanary), time.Now().UTC().Add(2*time.Second))
	resultCh := make(chan protocolv1.AskResult, 1)
	errCh := make(chan error, 1)
	go func() {
		result, err := requester.Submit(t.Context(), request)
		resultCh <- result
		errCh <- err
	}()
	pending := waitForPending(t, provider, request.ID)
	if pending.Metadata.BodyBytes != len(bodyCanary) || pending.Metadata.BodySHA256 != protocolv1.BodySHA256([]byte(bodyCanary)) {
		t.Fatalf("pending metadata = %#v", pending)
	}
	select {
	case <-jobs:
		t.Fatal("write reached provider runtime before approval")
	case <-time.After(50 * time.Millisecond):
	}
	assertRelayCanariesAbsent(t, dbPath, logs.String(), bodyCanary, answerCanary, pathCanary)
	if _, err := provider.ApproveRequest(t.Context(), request.ID, "deadbeef"); err != nil {
		t.Fatalf("ApproveRequest: %v", err)
	}
	job := <-jobs
	if !bytes.Equal(job.body, []byte(bodyCanary)) || job.commit != "deadbeef" {
		t.Fatalf("provider job = %#v", job)
	}
	result := <-resultCh
	if err := <-errCh; err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if result.Response == nil || !bytes.Equal(result.Response.Answer, []byte(answerCanary)) || result.Response.Worktree == nil || result.Response.Worktree.BaseCommit != "deadbeef" {
		t.Fatalf("write result = %#v", result)
	}
	assertRelayCanariesAbsent(t, dbPath, logs.String(), bodyCanary, answerCanary, pathCanary)

	denied := writeRequest("req_write_deny", member, agent.ID, []byte("deny-body"), time.Now().UTC().Add(2*time.Second))
	denyErr := make(chan error, 1)
	go func() { _, err := requester.Submit(t.Context(), denied); denyErr <- err }()
	waitForPending(t, provider, denied.ID)
	if _, err := provider.DenyRequest(t.Context(), denied.ID); err != nil {
		t.Fatal(err)
	}
	var remote *Error
	if err := <-denyErr; !errors.As(err, &remote) || remote.Code != "write_request_denied" {
		t.Fatalf("deny error = %v", err)
	}
	if invocations.Load() != 1 {
		t.Fatalf("provider invocations after deny = %d", invocations.Load())
	}
}

func TestWriteApprovalExpiryUnauthorizedApprovalAndPendingACLRevoke(t *testing.T) {
	store, err := relay.OpenStore(filepath.Join(t.TempDir(), "relay.sqlite"), slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	server, _ := relay.NewServer(store, slog.Default(), relay.WithWriteTimeout(time.Second))
	httpServer := httptest.NewServer(server.Handler())
	t.Cleanup(httpServer.Close)
	provider, requester, agent, member := setupWriteProject(t, store, httpServer.URL)
	providerCtx, stop := context.WithCancel(t.Context())
	ready := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- provider.ServeAgentWithJobHandler(providerCtx, agent.ID, func(map[string]any) { close(ready) }, nil)
	}()
	<-ready
	t.Cleanup(func() { stop(); <-done })

	expired := writeRequest("req_write_expire", member, agent.ID, []byte("expire"), time.Now().UTC().Add(80*time.Millisecond))
	expiredErr := make(chan error, 1)
	go func() { _, err := requester.Submit(t.Context(), expired); expiredErr <- err }()
	waitForPending(t, provider, expired.ID)
	var remote *Error
	if err := <-expiredErr; !errors.As(err, &remote) || remote.Code != "write_approval_expired" {
		t.Fatalf("expiry error = %v", err)
	}
	if _, err := provider.ApproveRequest(t.Context(), expired.ID, "HEAD"); !errors.As(err, &remote) || remote.Code != "request_not_pending" {
		t.Fatalf("approve expired = %v", err)
	}

	pendingRequest := writeRequest("req_write_acl", member, agent.ID, []byte("acl"), time.Now().UTC().Add(time.Second))
	aclErr := make(chan error, 1)
	go func() { _, err := requester.Submit(t.Context(), pendingRequest); aclErr <- err }()
	waitForPending(t, provider, pendingRequest.ID)
	if _, err := requester.ApproveRequest(t.Context(), pendingRequest.ID, "HEAD"); !errors.As(err, &remote) || remote.Status != 403 {
		t.Fatalf("requester approved provider request: %v", err)
	}
	if _, err := provider.SetAccess(t.Context(), agent.ID, member.ID, []protocolv1.RequestMode{protocolv1.ModeWrite}, false); err != nil {
		t.Fatal(err)
	}
	if err := <-aclErr; !errors.As(err, &remote) || remote.Code != "agent_access_revoked" {
		t.Fatalf("pending ACL revoke error = %v", err)
	}
}

func TestConcurrentWriteApproveAndDenyHaveExactlyOneWinner(t *testing.T) {
	store, err := relay.OpenStore(filepath.Join(t.TempDir(), "relay.sqlite"), slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	server, _ := relay.NewServer(store, slog.Default(), relay.WithWriteTimeout(time.Second))
	httpServer := httptest.NewServer(server.Handler())
	t.Cleanup(httpServer.Close)
	provider, requester, agent, member := setupWriteProject(t, store, httpServer.URL)
	providerCtx, stop := context.WithCancel(t.Context())
	ready := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- provider.ServeAgentWithJobHandler(providerCtx, agent.ID, func(map[string]any) { close(ready) }, func(_ context.Context, request protocolv1.Request, _ string) (protocolv1.Response, *protocolv1.RequestFailure) {
			return protocolv1.Response{SchemaVersion: 1, RequestID: request.ID, Status: protocolv1.StatusSucceeded, Answer: []byte("approved")}, nil
		})
	}()
	<-ready
	t.Cleanup(func() { stop(); <-done })

	for index := 0; index < 12; index++ {
		request := writeRequest(fmt.Sprintf("req_approval_race_%02d", index), member, agent.ID, []byte("race"), time.Now().UTC().Add(time.Second))
		requestDone := make(chan error, 1)
		go func() { _, err := requester.Submit(t.Context(), request); requestDone <- err }()
		waitForPending(t, provider, request.ID)
		start := make(chan struct{})
		results := make(chan error, 2)
		go func() { <-start; _, err := provider.ApproveRequest(t.Context(), request.ID, "HEAD"); results <- err }()
		go func() { <-start; _, err := provider.DenyRequest(t.Context(), request.ID); results <- err }()
		close(start)
		first, second := <-results, <-results
		winners := 0
		if first == nil {
			winners++
		}
		if second == nil {
			winners++
		}
		if winners != 1 {
			t.Fatalf("approve/deny winners=%d errors=(%v, %v)", winners, first, second)
		}
		_ = <-requestDone
		metadata, err := requester.GetRequest(t.Context(), request.ID)
		if err != nil || (metadata.Status != protocolv1.StatusSucceeded && metadata.Status != protocolv1.StatusDenied) {
			t.Fatalf("race metadata=%#v err=%v", metadata, err)
		}
	}
}

func setupWriteProject(t *testing.T, store *relay.Store, baseURL string) (*Client, *Client, relay.Agent, relay.Member) {
	t.Helper()
	_, _, ownerToken, err := store.CreateProject(t.Context(), "write-e2e", "owner")
	if err != nil {
		t.Fatal(err)
	}
	owner, _ := store.Authenticate(t.Context(), ownerToken)
	_, invite, _ := store.CreateInvite(t.Context(), owner, time.Hour)
	_, requester, requesterToken, err := store.JoinInvite(t.Context(), invite, "requester")
	if err != nil {
		t.Fatal(err)
	}
	agent, err := store.RegisterAgent(t.Context(), owner, protocolv1.AgentManifest{SchemaVersion: 1, Name: "writer", Summary: "Writer", Modes: []protocolv1.RequestMode{protocolv1.ModeWrite}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.SetACL(t.Context(), owner, agent.ID, requester.ID, []protocolv1.RequestMode{protocolv1.ModeWrite}, true); err != nil {
		t.Fatal(err)
	}
	provider, _ := New(baseURL, ownerToken)
	requesterClient, _ := New(baseURL, requesterToken)
	return provider, requesterClient, agent, requester
}

func writeRequest(id string, requester relay.Member, agentID string, body []byte, expires time.Time) protocolv1.Request {
	return protocolv1.Request{SchemaVersion: 1, ID: id, ProjectID: requester.ProjectID, RequesterID: requester.ID, AgentID: agentID, Mode: protocolv1.ModeWrite, Body: append([]byte(nil), body...), BodySHA256: protocolv1.BodySHA256(body), CreatedAt: time.Now().UTC(), ExpiresAt: &expires}
}

func waitForPending(t *testing.T, client *Client, requestID string) protocolv1.PendingRequest {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		pending, err := client.PendingRequests(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		for _, item := range pending {
			if item.Metadata.ID == requestID {
				return item
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("request %s did not become pending", requestID)
	return protocolv1.PendingRequest{}
}

func assertRelayCanariesAbsent(t *testing.T, dbPath, logs string, canaries ...string) {
	t.Helper()
	var combined strings.Builder
	combined.WriteString(logs)
	for _, path := range []string{dbPath, dbPath + "-wal", dbPath + "-shm"} {
		if data, err := os.ReadFile(path); err == nil {
			combined.Write(data)
		}
	}
	for _, canary := range canaries {
		if strings.Contains(combined.String(), canary) {
			t.Fatalf("Relay database/log contains canary %q", canary)
		}
	}
}
