package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wWzZb/peercontext/internal/localstate"
	"github.com/wWzZb/peercontext/internal/relay"
	"github.com/wWzZb/peercontext/internal/relayclient"
	"github.com/wWzZb/peercontext/internal/worktree"
	"github.com/wWzZb/peercontext/pkg/clioutput"
	protocolv1 "github.com/wWzZb/peercontext/pkg/protocol/v1"
)

func TestTaskCLIConfirmationBindsExactBodyBeforeRelaySubmission(t *testing.T) {
	fixture := setupWriteCLI(t)
	body := []byte{'w', 'r', 'i', 't', 'e', '\r', '\n', 0, 0xff}
	var stdout, stderr bytes.Buffer
	code := Run([]string{"task", fixture.agent.ID, "--mode", "write", "--approval-timeout", "2s", "--request-id", "req_cli_write"}, bytes.NewReader(body), &stdout, &stderr)
	if code != clioutput.ExitConfirmationRequired || stdout.Len() != 0 || fixture.requestPosts.Load() != 0 {
		t.Fatalf("first task code=%d stdout=%s stderr=%s posts=%d", code, stdout.String(), stderr.String(), fixture.requestPosts.Load())
	}
	confirmation, token := decodeConfirmationError(t, stderr.Bytes())
	if confirmation.AgentID != fixture.agent.ID || confirmation.Mode != protocolv1.ModeWrite || confirmation.BodyBytes != len(body) || confirmation.BodySHA256 != protocolv1.BodySHA256(body) {
		t.Fatalf("confirmation = %#v", confirmation)
	}

	stdout.Reset()
	stderr.Reset()
	code = Run([]string{"task", fixture.agent.ID, "--mode", "write", "--confirm", token, "--request-id", "req_cli_mismatch"}, bytes.NewReader([]byte("changed")), &stdout, &stderr)
	if code != clioutput.ExitAuthorization || fixture.requestPosts.Load() != 0 {
		t.Fatalf("mismatch code=%d stderr=%s posts=%d", code, stderr.String(), fixture.requestPosts.Load())
	}
	var mismatch clioutput.ErrorEnvelope
	if err := json.Unmarshal(stderr.Bytes(), &mismatch); err != nil || mismatch.Error.Code != "write_confirmation_mismatch" {
		t.Fatalf("mismatch envelope = %#v, %v", mismatch, err)
	}

	runDone := make(chan struct {
		code   clioutput.ExitCode
		stdout string
		stderr string
	}, 1)
	go func() {
		var out, errOut bytes.Buffer
		resultCode := Run([]string{"task", fixture.agent.ID, "--mode", "write", "--confirm", token, "--request-id", "req_cli_write", "--run-timeout", "2s"}, bytes.NewReader(body), &out, &errOut)
		runDone <- struct {
			code   clioutput.ExitCode
			stdout string
			stderr string
		}{resultCode, out.String(), errOut.String()}
	}()
	waitCLIPending(t, fixture.provider, "req_cli_write")
	select {
	case <-fixture.jobs:
		t.Fatal("CLI write reached provider before approval")
	case <-time.After(40 * time.Millisecond):
	}
	if _, err := fixture.provider.ApproveRequest(t.Context(), "req_cli_write", "0123456789abcdef"); err != nil {
		t.Fatal(err)
	}
	job := <-fixture.jobs
	if !bytes.Equal(job.body, body) || job.commit != "0123456789abcdef" {
		t.Fatalf("job = %#v", job)
	}
	result := <-runDone
	if result.code != clioutput.ExitOK || result.stderr != "" || fixture.requestPosts.Load() != 1 {
		t.Fatalf("confirmed task = %#v posts=%d", result, fixture.requestPosts.Load())
	}
}

func TestTaskCLIDenialUsesExitEleven(t *testing.T) {
	fixture := setupWriteCLI(t)
	body := []byte("deny this write")
	var firstErr bytes.Buffer
	if code := Run([]string{"task", fixture.agent.ID, "--mode", "write", "--approval-timeout", "2s"}, bytes.NewReader(body), &bytes.Buffer{}, &firstErr); code != clioutput.ExitConfirmationRequired {
		t.Fatalf("confirmation code = %d", code)
	}
	_, token := decodeConfirmationError(t, firstErr.Bytes())
	done := make(chan struct {
		code clioutput.ExitCode
		err  string
	}, 1)
	go func() {
		var stderr bytes.Buffer
		code := Run([]string{"task", fixture.agent.ID, "--mode", "write", "--confirm", token, "--request-id", "req_cli_deny", "--run-timeout", "2s"}, bytes.NewReader(body), &bytes.Buffer{}, &stderr)
		done <- struct {
			code clioutput.ExitCode
			err  string
		}{code, stderr.String()}
	}()
	waitCLIPending(t, fixture.provider, "req_cli_deny")
	if _, err := fixture.provider.DenyRequest(t.Context(), "req_cli_deny"); err != nil {
		t.Fatal(err)
	}
	result := <-done
	if result.code != clioutput.ExitDenied {
		t.Fatalf("denied code=%d stderr=%s", result.code, result.err)
	}
	var envelope clioutput.ErrorEnvelope
	if err := json.Unmarshal([]byte(result.err), &envelope); err != nil || envelope.Error.Code != "write_request_denied" {
		t.Fatalf("denied envelope=%#v err=%v", envelope, err)
	}
}

func TestWorktreeCLIListsAndExplicitlyRemovesProviderLocalCheckout(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("PEERCTX_CONFIG_DIR", configDir)
	repository := filepath.Join(t.TempDir(), "repository")
	if err := os.MkdirAll(repository, 0700); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"init", "-b", "main"}, {"config", "user.name", "PeerContext Test"}, {"config", "user.email", "peerctx@example.invalid"}} {
		commandArgs := append([]string{"-C", repository}, args...)
		if output, err := exec.Command("git", commandArgs...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, output)
		}
	}
	if err := os.WriteFile(filepath.Join(repository, "fixture.txt"), []byte("fixture\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"add", "fixture.txt"}, {"commit", "-m", "fixture"}} {
		commandArgs := append([]string{"-C", repository}, args...)
		if output, err := exec.Command("git", commandArgs...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, output)
		}
	}
	manager, _ := worktree.New(configDir)
	record, err := manager.Create(repository, "agt_cli", "req_cli_worktree", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"worktree", "list"}, bytes.NewReader(nil), &stdout, &stderr); code != clioutput.ExitOK || !bytes.Contains(stdout.Bytes(), []byte(record.ID)) || !bytes.Contains(stdout.Bytes(), []byte(record.Path)) {
		t.Fatalf("worktree list code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	stdout.Reset()
	if code := Run([]string{"worktree", "remove", record.ID}, bytes.NewReader(nil), &stdout, &stderr); code != clioutput.ExitOK || !bytes.Contains(stdout.Bytes(), []byte(`"recoverable":false`)) {
		t.Fatalf("worktree remove code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if _, err := os.Stat(record.Path); !os.IsNotExist(err) {
		t.Fatalf("removed worktree still exists: %v", err)
	}
	stdout.Reset()
	stderr.Reset()
	if code := Run([]string{"worktree", "remove", record.ID}, bytes.NewReader(nil), &stdout, &stderr); code != clioutput.ExitNotFound {
		t.Fatalf("remove missing worktree code=%d stderr=%s", code, stderr.String())
	}
}

type writeCLIFixture struct {
	agent        relay.Agent
	provider     *relayclient.Client
	requestPosts atomic.Int32
	jobs         chan struct {
		body   []byte
		commit string
	}
}

func setupWriteCLI(t *testing.T) *writeCLIFixture {
	t.Helper()
	store, err := relay.OpenStore(filepath.Join(t.TempDir(), "relay.sqlite"), slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	server, _ := relay.NewServer(store, slog.Default(), relay.WithWriteTimeout(2*time.Second))
	fixture := &writeCLIFixture{jobs: make(chan struct {
		body   []byte
		commit string
	}, 2)}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/v1/requests" {
			fixture.requestPosts.Add(1)
		}
		server.Handler().ServeHTTP(w, r)
	})
	httpServer := httptest.NewServer(handler)
	t.Cleanup(httpServer.Close)
	project, _, ownerToken, err := store.CreateProject(t.Context(), "cli-write", "owner")
	if err != nil {
		t.Fatal(err)
	}
	owner, _ := store.Authenticate(t.Context(), ownerToken)
	_, invite, _ := store.CreateInvite(t.Context(), owner, time.Hour)
	_, requester, requesterToken, err := store.JoinInvite(t.Context(), invite, "requester")
	if err != nil {
		t.Fatal(err)
	}
	fixture.agent, err = store.RegisterAgent(t.Context(), owner, protocolv1.AgentManifest{SchemaVersion: 1, Name: "writer", Summary: "Writer", Modes: []protocolv1.RequestMode{protocolv1.ModeWrite}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.SetACL(t.Context(), owner, fixture.agent.ID, requester.ID, []protocolv1.RequestMode{protocolv1.ModeWrite}, true); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PEERCTX_CONFIG_DIR", t.TempDir())
	local, _ := localstate.DefaultManager()
	credentialFile := filepath.Join(t.TempDir(), "requester.token")
	if err = local.PrepareCredentialStore(credentialFile); err != nil {
		t.Fatal(err)
	}
	if err = local.PutProfile(localstate.Profile{ProjectID: project.ID, ProjectName: project.Name, RelayURL: httpServer.URL, MemberID: requester.ID, MemberName: requester.Name}, requesterToken, credentialFile); err != nil {
		t.Fatal(err)
	}
	fixture.provider, _ = relayclient.New(httpServer.URL, ownerToken)
	providerCtx, stop := context.WithCancel(t.Context())
	ready := make(chan struct{})
	providerDone := make(chan error, 1)
	go func() {
		providerDone <- fixture.provider.ServeAgentWithJobHandler(providerCtx, fixture.agent.ID, func(map[string]any) { close(ready) }, func(_ context.Context, request protocolv1.Request, commit string) (protocolv1.Response, *protocolv1.RequestFailure) {
			fixture.jobs <- struct {
				body   []byte
				commit string
			}{append([]byte(nil), request.Body...), commit}
			return protocolv1.Response{SchemaVersion: 1, RequestID: request.ID, Status: protocolv1.StatusSucceeded, Answer: []byte("approved answer")}, nil
		})
	}()
	<-ready
	t.Cleanup(func() { stop(); <-providerDone })
	return fixture
}

func decodeConfirmationError(t *testing.T, data []byte) (protocolv1.WriteConfirmation, string) {
	t.Helper()
	var envelope struct {
		Error struct {
			Code    string `json:"code"`
			Details struct {
				Confirmation protocolv1.WriteConfirmation `json:"confirmation"`
				Token        string                       `json:"confirmation_token"`
			} `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Error.Code != "write_confirmation_required" || envelope.Error.Details.Token == "" {
		t.Fatalf("confirmation error = %#v", envelope)
	}
	return envelope.Error.Details.Confirmation, envelope.Error.Details.Token
}

func waitCLIPending(t *testing.T, provider *relayclient.Client, requestID string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		pending, err := provider.PendingRequests(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		for _, item := range pending {
			if item.Metadata.ID == requestID {
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("request %s did not become pending", requestID)
}
