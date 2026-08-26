package relay

import (
	"bytes"
	"errors"
	"testing"
	"time"

	protocolv1 "github.com/wWzZb/peercontext/pkg/protocol/v1"
)

func TestBrokerRejectsOversizedAndWrongAgentResponses(t *testing.T) {
	store := openTestStore(t)
	broker := newBroker(store)
	now := time.Now().UTC()
	body := []byte("request")
	request := protocolv1.Request{SchemaVersion: 1, ID: "req_broker", ProjectID: "prj_test", RequesterID: "mem_test", AgentID: "agt_expected", Mode: protocolv1.ModeRead, Body: body, BodySHA256: protocolv1.BodySHA256(body), CreatedAt: now}
	metadata, _, err := store.BeginRequest(t.Context(), request, protocolv1.StatusRunning, now)
	if err != nil {
		t.Fatal(err)
	}
	active := &activeRequest{metadata: metadata, done: make(chan struct{})}
	broker.active[request.ID] = active
	broker.completeResponse("agt_wrong", protocolv1.Response{SchemaVersion: 1, RequestID: request.ID, Status: protocolv1.StatusSucceeded, Answer: []byte("forged")})
	select {
	case <-active.done:
		t.Fatal("wrong Agent completed another Agent's request")
	case <-time.After(20 * time.Millisecond):
	}
	oversized := bytes.Repeat([]byte{'x'}, protocolv1.MaxResponseBodyBytes+1)
	broker.completeResponse("agt_expected", protocolv1.Response{SchemaVersion: 1, RequestID: request.ID, Status: protocolv1.StatusSucceeded, Answer: oversized})
	outcome, err := active.wait(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if outcome.failure == nil || outcome.failure.Code != "response_too_large" {
		t.Fatalf("outcome = %#v", outcome)
	}
	stored, _ := store.GetRequestMetadata(t.Context(), request.ID)
	if stored.Status != protocolv1.StatusFailed {
		t.Fatalf("status = %s", stored.Status)
	}
}

func TestActiveWriteBodyExistsOnlyUntilApprovalOrTerminalState(t *testing.T) {
	now := time.Now().UTC()
	body := []byte("PEERCTX_PENDING_MEMORY_ONLY_CANARY")
	expires := now.Add(time.Minute)
	request := protocolv1.Request{SchemaVersion: 1, ID: "req_pending_memory", ProjectID: "prj_test", RequesterID: "mem_test", AgentID: "agt_test", Mode: protocolv1.ModeWrite, Body: body, BodySHA256: protocolv1.BodySHA256(body), CreatedAt: now, ExpiresAt: &expires}
	active := &activeRequest{metadata: request.Metadata(protocolv1.StatusPendingApproval, now), pendingRequest: &request, approvalExpires: expires, done: make(chan struct{})}
	if _, ok := active.pendingSnapshot(now); !ok {
		t.Fatal("write did not enter pending approval")
	}
	dispatched, err := active.takePending(now.Add(time.Second))
	if err != nil || !bytes.Equal(dispatched.Body, body) {
		t.Fatalf("takePending = %#v, %v", dispatched, err)
	}
	active.mu.Lock()
	pendingBodyCleared := active.pendingRequest == nil
	active.mu.Unlock()
	if !pendingBodyCleared || !active.providerStarted() {
		t.Fatal("pending body was retained after approval")
	}
	if _, err := active.takePending(now.Add(2 * time.Second)); !errors.Is(err, ErrRequestNotPending) {
		t.Fatalf("second approval error = %v", err)
	}
}
