package service

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/wWzZb/peercontext/internal/failure"
	"github.com/wWzZb/peercontext/internal/v2state"
	protocolv2 "github.com/wWzZb/peercontext/pkg/protocol/v2"
)

type fakeRuntime struct {
	mu     sync.Mutex
	bodies [][]byte
	answer []byte
}

func (f *fakeRuntime) Read(_ context.Context, _ string, body []byte) ([]byte, error) {
	f.mu.Lock()
	f.bodies = append(f.bodies, append([]byte(nil), body...))
	f.mu.Unlock()
	return append([]byte(nil), f.answer...), nil
}

func TestTwoDeviceLANFirstActivationAndRead(t *testing.T) {
	t.Setenv("PEERCTX_ALLOW_UNSUPPORTED", "1")
	t.Setenv("PEERCTX_DISABLE_MDNS", "1")
	t.Setenv("PEERCTX_INCLUDE_LOOPBACK", "1")
	root, err := os.MkdirTemp("", "pc2-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	t.Setenv("PEERCTX_TEST_KEY_DIR", filepath.Join(root, "keys"))
	hostManager, _ := v2state.NewManager(filepath.Join(root, "host", "v2"))
	peerManager, _ := v2state.NewManager(filepath.Join(root, "peer", "v2"))
	hostRuntime := &fakeRuntime{answer: []byte("unused")}
	peerRuntime := &fakeRuntime{answer: []byte("verified remote answer")}
	hostCancel, hostDone := runTestDaemon(t, hostManager, hostRuntime)
	defer func() { hostCancel(); <-hostDone }()
	peerCancel, peerDone := runTestDaemon(t, peerManager, peerRuntime)
	peerStopped := false
	defer func() {
		if !peerStopped {
			peerCancel()
			<-peerDone
		}
	}()

	hostControl := NewControlClient(hostManager.SocketPath())
	peerControl := NewControlClient(peerManager.SocketPath())
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var created ProjectCreateResult
	if err := hostControl.Do(ctx, ActionProjectCreate, ProjectCreateInput{Name: "SDK", MemberName: "Alice"}, &created); err != nil {
		t.Fatalf("create Project: %v", err)
	}
	if len(created.Invitation) < len(protocolv2.InvitationPrefix) || created.Invitation[:len(protocolv2.InvitationPrefix)] != protocolv2.InvitationPrefix {
		t.Fatalf("invitation = %q", created.Invitation)
	}
	var joined ProjectJoinResult
	if err := peerControl.Do(ctx, ActionProjectJoin, ProjectJoinInput{Invitation: created.Invitation, MemberName: "Bob"}, &joined); err != nil {
		t.Fatalf("join Project using invitation only: %v", err)
	}
	if joined.Project.ID != created.Project.ID {
		t.Fatalf("joined Project %q, want %q", joined.Project.ID, created.Project.ID)
	}
	if err := peerControl.Do(ctx, ActionProjectJoin, ProjectJoinInput{Invitation: created.Invitation, MemberName: "Charlie"}, &ProjectJoinResult{}); failureCode(err) != "invite_consumed" {
		t.Fatalf("second invitation use code = %q, error = %v", failureCode(err), err)
	}
	repository := filepath.Join(root, "peer-repository")
	if err := os.MkdirAll(repository, 0700); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command("git", "init", repository).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, output)
	}
	var agent protocolv2.Agent
	if err := peerControl.Do(ctx, ActionAgentRegister, AgentRegisterInput{Repository: repository}, &agent); err != nil {
		t.Fatalf("register Agent: %v", err)
	}
	if !agent.Online || agent.Manifest.Name != "Bob/peer-repository" {
		t.Fatalf("registered Agent = %#v", agent)
	}

	requestBody := []byte("exact\x00request\nwith bytes")
	var response protocolv2.Response
	if err := hostControl.Do(ctx, ActionAsk, AskInput{Agent: agent.ID, Body: requestBody, TimeoutMS: 5000}, &response); err != nil {
		t.Fatalf("first read: %v", err)
	}
	if !bytes.Equal(response.Answer, peerRuntime.answer) {
		t.Fatalf("answer = %q", response.Answer)
	}
	peerRuntime.mu.Lock()
	if len(peerRuntime.bodies) != 1 || !bytes.Equal(peerRuntime.bodies[0], requestBody) {
		t.Fatalf("runtime stdin = %#v", peerRuntime.bodies)
	}
	peerRuntime.mu.Unlock()
	databaseBytes, err := os.ReadFile(hostManager.DatabasePath())
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(databaseBytes, requestBody) || bytes.Contains(databaseBytes, response.Answer) || bytes.Contains(databaseBytes, []byte(repository)) {
		t.Fatal("host database persisted request, answer, or repository path")
	}

	peerCancel()
	if err := <-peerDone; err != nil {
		t.Fatalf("stop peer daemon: %v", err)
	}
	peerStopped = true
	deadline := time.Now().Add(2 * time.Second)
	for {
		err := hostControl.Do(ctx, ActionAsk, AskInput{Agent: agent.ID, Body: []byte("offline check"), TimeoutMS: 250}, &protocolv2.Response{})
		if failureCode(err) == "agent_offline" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("offline Agent code = %q, error = %v", failureCode(err), err)
		}
		time.Sleep(25 * time.Millisecond)
	}
}

func failureCode(err error) string {
	var structured *failure.Error
	if errors.As(err, &structured) {
		return structured.Code
	}
	return ""
}

func runTestDaemon(t *testing.T, manager *v2state.Manager, runtime Runtime) (context.CancelFunc, <-chan error) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- NewDaemon(manager, runtime).Run(ctx) }()
	client := NewControlClient(manager.SocketPath())
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case err := <-done:
			t.Fatalf("test daemon exited during startup: %v", err)
		default:
		}
		pingCtx, pingCancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		err := client.Do(pingCtx, ActionStatus, struct{}{}, nil)
		pingCancel()
		if err == nil {
			return cancel, done
		}
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	t.Fatalf("test daemon did not start")
	return nil, nil
}
