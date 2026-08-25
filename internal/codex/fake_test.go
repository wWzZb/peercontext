package codex

import (
	"bytes"
	"context"
	"errors"
	"testing"

	protocolv1 "github.com/wWzZb/peercontext/pkg/protocol/v1"
)

func TestFakeAdapterPreservesStdinAndResponseBytes(t *testing.T) {
	stdin := []byte{'r', 'a', 'w', '\r', '\n', 0, 0xff}
	response := []byte{'o', 'k', '\n', 0xfe}
	fake := &FakeAdapter{Response: response}

	result, err := fake.Run(context.Background(), Invocation{
		Workspace: "/detached/worktree",
		Mode:      protocolv1.ModeRead,
		Stdin:     bytes.NewReader(stdin),
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !bytes.Equal(result.FinalMessage, response) {
		t.Fatalf("response = %v, want %v", result.FinalMessage, response)
	}
	invocations := fake.Invocations()
	if len(invocations) != 1 {
		t.Fatalf("invocation count = %d, want 1", len(invocations))
	}
	if !bytes.Equal(invocations[0].Stdin, stdin) {
		t.Fatalf("stdin = %v, want %v", invocations[0].Stdin, stdin)
	}

	stdin[0] = 'X'
	response[0] = 'X'
	result.FinalMessage[1] = 'X'
	invocations[0].Stdin[1] = 'X'
	secondRead := fake.Invocations()
	if bytes.Equal(secondRead[0].Stdin, stdin) {
		t.Fatal("recorded stdin aliases caller memory")
	}
	if !bytes.Equal(secondRead[0].Stdin, []byte{'r', 'a', 'w', '\r', '\n', 0, 0xff}) {
		t.Fatalf("recorded stdin mutated: %v", secondRead[0].Stdin)
	}
}

func TestFakeAdapterReturnsConfiguredError(t *testing.T) {
	wantErr := errors.New("fake codex failed")
	fake := &FakeAdapter{Err: wantErr}

	_, err := fake.Run(context.Background(), Invocation{Stdin: bytes.NewReader(nil)})
	if !errors.Is(err, wantErr) {
		t.Fatalf("Run error = %v, want %v", err, wantErr)
	}
}

func TestFakeAdapterHonorsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	fake := &FakeAdapter{}

	_, err := fake.Run(ctx, Invocation{Stdin: bytes.NewReader(nil)})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Run error = %v, want context.Canceled", err)
	}
	if len(fake.Invocations()) != 0 {
		t.Fatal("canceled invocation was recorded")
	}
}
