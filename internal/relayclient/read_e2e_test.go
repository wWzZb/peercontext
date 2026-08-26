package relayclient

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wWzZb/peercontext/internal/codex"
	"github.com/wWzZb/peercontext/internal/relay"
	protocolv1 "github.com/wWzZb/peercontext/pkg/protocol/v1"
)

func TestReadEndToEndPreservesBytesAndDoesNotPersistBodies(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "relay.sqlite")
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	store, err := relay.OpenStore(dbPath, logger)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	server, _ := relay.NewServer(store, logger, relay.WithReadTimeout(2*time.Second))
	httpServer := httptest.NewServer(server.Handler())
	t.Cleanup(httpServer.Close)

	providerClient, requesterClient, agent, requester := setupReadProject(t, store, httpServer.URL)
	requestBody := []byte{'P', 'E', 'E', 'R', 'C', 'T', 'X', '_', 'B', 'O', 'D', 'Y', '\r', '\n', 0, 0xff}
	answer := []byte("PEERCTX_ANSWER_CANARY_4a930d\n")
	fake := &codex.FakeAdapter{Response: answer}
	providerCtx, stopProvider := context.WithCancel(t.Context())
	ready := make(chan struct{})
	providerDone := make(chan error, 1)
	go func() {
		providerDone <- providerClient.ServeAgentWithHandler(providerCtx, agent.ID, func(map[string]any) { close(ready) }, func(ctx context.Context, request protocolv1.Request) (protocolv1.Response, *protocolv1.RequestFailure) {
			result, err := fake.Run(ctx, codex.Invocation{Workspace: "/provider/repository", Mode: request.Mode, Stdin: bytes.NewReader(request.Body)})
			if err != nil {
				return protocolv1.Response{}, &protocolv1.RequestFailure{SchemaVersion: 1, RequestID: request.ID, Code: "fake_codex_failed", Message: "Fake Codex failed."}
			}
			return protocolv1.Response{SchemaVersion: 1, RequestID: request.ID, Status: protocolv1.StatusSucceeded, Answer: result.FinalMessage}, nil
		})
	}()
	select {
	case <-ready:
	case <-time.After(2 * time.Second):
		t.Fatal("provider did not become ready")
	}
	t.Cleanup(func() {
		stopProvider()
		select {
		case <-providerDone:
		case <-time.After(time.Second):
		}
	})

	now := time.Now().UTC()
	expires := now.Add(time.Minute)
	request := protocolv1.Request{SchemaVersion: 1, ID: "req_binary_e2e", ProjectID: requester.ProjectID, RequesterID: requester.ID, AgentID: agent.ID, Mode: protocolv1.ModeRead, Body: requestBody, BodySHA256: protocolv1.BodySHA256(requestBody), CreatedAt: now, ExpiresAt: &expires}
	result, err := requesterClient.Ask(t.Context(), request)
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}
	if result.Response == nil || !bytes.Equal(result.Response.Answer, answer) {
		t.Fatalf("answer = %#v", result.Response)
	}
	invocations := fake.Invocations()
	if len(invocations) != 1 || !bytes.Equal(invocations[0].Stdin, requestBody) {
		t.Fatalf("invocations = %#v", invocations)
	}
	metadata, err := requesterClient.GetRequest(t.Context(), request.ID)
	if err != nil || metadata.Status != protocolv1.StatusSucceeded {
		t.Fatalf("metadata = %#v, %v", metadata, err)
	}

	replay, err := requesterClient.Ask(t.Context(), request)
	if err != nil || !replay.Replayed || replay.Response != nil || replay.Metadata == nil || replay.Metadata.Status != protocolv1.StatusSucceeded {
		t.Fatalf("replay = %#v, %v", replay, err)
	}
	request.Body = []byte("different body")
	request.BodySHA256 = protocolv1.BodySHA256(request.Body)
	_, err = requesterClient.Ask(t.Context(), request)
	var remote *Error
	if !errors.As(err, &remote) || remote.Code != "request_replay_mismatch" {
		t.Fatalf("mismatched replay error = %v", err)
	}

	database, err := os.ReadFile(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	combined := string(database) + logs.String()
	for _, canary := range []string{string(requestBody), string(answer), "/provider/repository"} {
		if strings.Contains(combined, canary) {
			t.Fatalf("Relay database/log contains canary %q", canary)
		}
	}
}

func TestReadTimeoutAndExplicitCancelStopProviderContext(t *testing.T) {
	store, err := relay.OpenStore(filepath.Join(t.TempDir(), "relay.sqlite"), slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	server, _ := relay.NewServer(store, slog.Default(), relay.WithReadTimeout(80*time.Millisecond))
	httpServer := httptest.NewServer(server.Handler())
	t.Cleanup(httpServer.Close)
	providerClient, requesterClient, agent, requester := setupReadProject(t, store, httpServer.URL)
	started := make(chan string, 2)
	canceled := make(chan string, 2)
	providerCtx, stop := context.WithCancel(t.Context())
	ready := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- providerClient.ServeAgentWithHandler(providerCtx, agent.ID, func(map[string]any) { close(ready) }, func(ctx context.Context, request protocolv1.Request) (protocolv1.Response, *protocolv1.RequestFailure) {
			started <- request.ID
			<-ctx.Done()
			canceled <- request.ID
			return protocolv1.Response{}, &protocolv1.RequestFailure{SchemaVersion: 1, RequestID: request.ID, Code: "codex_canceled", Message: "Codex was canceled."}
		})
	}()
	<-ready
	t.Cleanup(func() { stop(); <-done })

	makeRequest := func(id string) protocolv1.Request {
		now := time.Now().UTC()
		expires := now.Add(time.Minute)
		body := []byte(id)
		return protocolv1.Request{SchemaVersion: 1, ID: id, ProjectID: requester.ProjectID, RequesterID: requester.ID, AgentID: agent.ID, Mode: protocolv1.ModeRead, Body: body, BodySHA256: protocolv1.BodySHA256(body), CreatedAt: now, ExpiresAt: &expires}
	}
	_, err = requesterClient.Ask(t.Context(), makeRequest("req_timeout"))
	var timeoutErr *Error
	if !errors.As(err, &timeoutErr) || timeoutErr.Code != "request_timeout" {
		t.Fatalf("timeout error = %v", err)
	}
	if id := <-canceled; id != "req_timeout" {
		t.Fatalf("canceled id = %s", id)
	}
	metadata, _ := requesterClient.GetRequest(t.Context(), "req_timeout")
	if metadata.Status != protocolv1.StatusExpired {
		t.Fatalf("timeout status = %s", metadata.Status)
	}

	askDone := make(chan error, 1)
	go func() { _, err := requesterClient.Ask(t.Context(), makeRequest("req_cancel")); askDone <- err }()
	if id := <-started; id != "req_timeout" { // timeout start may still be queued before its cancellation signal
		if id != "req_cancel" {
			t.Fatalf("started id = %s", id)
		}
	} else {
		if id := <-started; id != "req_cancel" {
			t.Fatalf("started id = %s", id)
		}
	}
	if _, err := requesterClient.CancelRequest(t.Context(), "req_cancel"); err != nil {
		t.Fatalf("CancelRequest: %v", err)
	}
	var cancelErr *Error
	if err := <-askDone; !errors.As(err, &cancelErr) || cancelErr.Code != "request_canceled" {
		t.Fatalf("ask cancel error = %v", err)
	}
	if id := <-canceled; id != "req_cancel" {
		t.Fatalf("provider canceled id = %s", id)
	}
	metadata, _ = requesterClient.GetRequest(t.Context(), "req_cancel")
	if metadata.Status != protocolv1.StatusCanceled {
		t.Fatalf("cancel status = %s", metadata.Status)
	}

	askDone = make(chan error, 1)
	go func() { _, err := requesterClient.Ask(t.Context(), makeRequest("req_acl_revoke")); askDone <- err }()
	if id := <-started; id != "req_acl_revoke" {
		t.Fatalf("started id = %s", id)
	}
	if _, err := providerClient.SetAccess(t.Context(), agent.ID, requester.ID, []protocolv1.RequestMode{protocolv1.ModeRead}, false); err != nil {
		t.Fatalf("revoke ACL: %v", err)
	}
	var aclErr *Error
	if err := <-askDone; !errors.As(err, &aclErr) || aclErr.Code != "agent_access_revoked" {
		t.Fatalf("ACL revoke ask error = %v", err)
	}
	if id := <-canceled; id != "req_acl_revoke" {
		t.Fatalf("provider canceled id = %s", id)
	}
	metadata, _ = requesterClient.GetRequest(t.Context(), "req_acl_revoke")
	if metadata.Status != protocolv1.StatusCanceled {
		t.Fatalf("ACL revoke status = %s", metadata.Status)
	}
}

func TestOfflineReadFailsImmediatelyWithoutQueue(t *testing.T) {
	store, err := relay.OpenStore(filepath.Join(t.TempDir(), "relay.sqlite"), slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	server, _ := relay.NewServer(store, slog.Default())
	httpServer := httptest.NewServer(server.Handler())
	t.Cleanup(httpServer.Close)
	_, requesterClient, agent, requester := setupReadProject(t, store, httpServer.URL)
	now := time.Now().UTC()
	expires := now.Add(time.Minute)
	body := []byte("offline")
	request := protocolv1.Request{SchemaVersion: 1, ID: "req_offline", ProjectID: requester.ProjectID, RequesterID: requester.ID, AgentID: agent.ID, Mode: protocolv1.ModeRead, Body: body, BodySHA256: protocolv1.BodySHA256(body), CreatedAt: now, ExpiresAt: &expires}
	started := time.Now()
	_, err = requesterClient.Ask(t.Context(), request)
	if time.Since(started) > time.Second {
		t.Fatal("offline request did not fail immediately")
	}
	var remote *Error
	if !errors.As(err, &remote) || remote.Code != "agent_offline" || !remote.Retryable {
		t.Fatalf("offline error = %#v, %v", remote, err)
	}
	metadata, err := requesterClient.GetRequest(t.Context(), request.ID)
	if err != nil || metadata.Status != protocolv1.StatusFailed {
		t.Fatalf("offline metadata = %#v, %v", metadata, err)
	}
}

func setupReadProject(t *testing.T, store *relay.Store, baseURL string) (*Client, *Client, relay.Agent, relay.Member) {
	t.Helper()
	project, owner, ownerToken, err := store.CreateProject(t.Context(), "read-e2e", "owner")
	if err != nil {
		t.Fatal(err)
	}
	ownerPrincipal, _ := store.Authenticate(t.Context(), ownerToken)
	_, inviteToken, _ := store.CreateInvite(t.Context(), ownerPrincipal, time.Hour)
	_, requester, requesterToken, err := store.JoinInvite(t.Context(), inviteToken, "requester")
	if err != nil {
		t.Fatal(err)
	}
	manifest := protocolv1.AgentManifest{SchemaVersion: 1, Name: "provider", Summary: "Provider", Modes: []protocolv1.RequestMode{protocolv1.ModeRead}}
	agent, err := store.RegisterAgent(t.Context(), ownerPrincipal, manifest)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.SetACL(t.Context(), ownerPrincipal, agent.ID, requester.ID, []protocolv1.RequestMode{protocolv1.ModeRead}, true); err != nil {
		t.Fatal(err)
	}
	providerClient, _ := New(baseURL, ownerToken)
	requesterClient, _ := New(baseURL, requesterToken)
	_ = project
	_ = owner
	return providerClient, requesterClient, agent, requester
}
