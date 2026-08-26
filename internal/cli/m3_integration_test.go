package cli

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/wWzZb/peercontext/internal/codex"
	"github.com/wWzZb/peercontext/internal/localstate"
	"github.com/wWzZb/peercontext/internal/relay"
	"github.com/wWzZb/peercontext/internal/relayclient"
	"github.com/wWzZb/peercontext/pkg/clioutput"
	protocolv1 "github.com/wWzZb/peercontext/pkg/protocol/v1"
)

func TestAskCLIForwardsRawStdinAndReturnsRawAnswer(t *testing.T) {
	store, err := relay.OpenStore(filepath.Join(t.TempDir(), "relay.sqlite"), slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	server, _ := relay.NewServer(store, slog.Default())
	httpServer := httptest.NewServer(server.Handler())
	t.Cleanup(httpServer.Close)
	_, _, ownerToken, err := store.CreateProject(t.Context(), "cli-read", "owner")
	if err != nil {
		t.Fatal(err)
	}
	ownerPrincipal, _ := store.Authenticate(t.Context(), ownerToken)
	_, inviteToken, _ := store.CreateInvite(t.Context(), ownerPrincipal, time.Hour)
	project, requester, requesterToken, err := store.JoinInvite(t.Context(), inviteToken, "requester")
	if err != nil {
		t.Fatal(err)
	}
	agent, err := store.RegisterAgent(t.Context(), ownerPrincipal, protocolv1.AgentManifest{SchemaVersion: 1, Name: "provider", Summary: "Provider", Modes: []protocolv1.RequestMode{protocolv1.ModeRead}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.SetACL(t.Context(), ownerPrincipal, agent.ID, requester.ID, []protocolv1.RequestMode{protocolv1.ModeRead}, true); err != nil {
		t.Fatal(err)
	}

	configDir := t.TempDir()
	t.Setenv("PEERCTX_CONFIG_DIR", configDir)
	manager, _ := localstate.DefaultManager()
	credentialFile := filepath.Join(t.TempDir(), "requester.token")
	if err = manager.PrepareCredentialStore(credentialFile); err != nil {
		t.Fatal(err)
	}
	if err = manager.PutProfile(localstate.Profile{ProjectID: project.ID, ProjectName: project.Name, RelayURL: httpServer.URL, MemberID: requester.ID, MemberName: requester.Name}, requesterToken, credentialFile); err != nil {
		t.Fatal(err)
	}

	body := []byte{'b', 'i', 'n', 'a', 'r', 'y', '\r', '\n', 0, 0xff}
	answer := []byte{'a', 'n', 's', 'w', 'e', 'r', '\n', 0xfe}
	fake := &codex.FakeAdapter{Response: answer}
	providerClient, _ := relayclient.New(httpServer.URL, ownerToken)
	providerCtx, stop := context.WithCancel(t.Context())
	ready := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- providerClient.ServeAgentWithHandler(providerCtx, agent.ID, func(map[string]any) { close(ready) }, func(ctx context.Context, request protocolv1.Request) (protocolv1.Response, *protocolv1.RequestFailure) {
			result, runErr := fake.Run(ctx, codex.Invocation{Workspace: "/provider/repository", Mode: request.Mode, Stdin: bytes.NewReader(request.Body)})
			if runErr != nil {
				t.Errorf("fake Run: %v", runErr)
			}
			return protocolv1.Response{SchemaVersion: 1, RequestID: request.ID, Status: protocolv1.StatusSucceeded, Answer: result.FinalMessage}, nil
		})
	}()
	<-ready
	t.Cleanup(func() { stop(); <-done })

	var stdout, stderr bytes.Buffer
	code := Run([]string{"ask", agent.ID, "--mode", "read", "--timeout", "2s", "--request-id", "req_cli_binary"}, bytes.NewReader(body), &stdout, &stderr)
	if code != clioutput.ExitOK {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	var envelope struct {
		OK   bool `json:"ok"`
		Data struct {
			Response struct {
				Answer string `json:"answer"`
			} `json:"response"`
		} `json:"data"`
		Meta struct {
			RequestID string `json:"request_id"`
		} `json:"meta"`
	}
	if err = json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	decoded, err := base64.StdEncoding.DecodeString(envelope.Data.Response.Answer)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(decoded, answer) {
		t.Fatalf("answer = %v", decoded)
	}
	if envelope.Meta.RequestID != "req_cli_binary" {
		t.Fatalf("request id = %s", envelope.Meta.RequestID)
	}
	invocations := fake.Invocations()
	if len(invocations) != 1 || !bytes.Equal(invocations[0].Stdin, body) {
		t.Fatalf("stdin = %#v", invocations)
	}
}

type panicReader struct{ t *testing.T }

func (p panicReader) Read([]byte) (int, error) {
	p.t.Fatal("recursive request read stdin before blocking")
	return 0, nil
}

func TestRecursiveAskAndTaskAreBlockedBeforeReadingStdin(t *testing.T) {
	t.Setenv("PEERCTX_INBOUND_REQUEST", "1")
	for _, command := range [][]string{{"ask", "agent", "--mode", "read"}, {"task", "agent", "--mode", "write"}} {
		var stdout, stderr bytes.Buffer
		code := Run(command, panicReader{t}, &stdout, &stderr)
		if code != clioutput.ExitAuthorization {
			t.Fatalf("Run(%v) code=%d stderr=%s", command, code, stderr.String())
		}
		var envelope clioutput.ErrorEnvelope
		if err := json.Unmarshal(stderr.Bytes(), &envelope); err != nil {
			t.Fatal(err)
		}
		if envelope.Error.Code != "recursive_request_blocked" {
			t.Fatalf("Run(%v) error=%#v", command, envelope.Error)
		}
	}
}
