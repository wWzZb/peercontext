package codex

import (
	"bytes"
	"context"
	"errors"
	"testing"
)

func TestFakeAdapterPreservesStdinAndResponseBytes(t *testing.T) {
	stdin := []byte{'r', 'a', 'w', '\r', '\n', 0, 0xff}
	response := []byte{'o', 'k', '\n', 0xfe}
	fake := &FakeAdapter{Response: response}
	result, err := fake.Run(context.Background(), Invocation{Workspace: "/repository", Stdin: bytes.NewReader(stdin)})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(result.FinalMessage, response) || !bytes.Equal(fake.Invocations()[0].Stdin, stdin) {
		t.Fatal("fake adapter changed request or response bytes")
	}
	stdin[0], response[0], result.FinalMessage[1] = 'X', 'X', 'X'
	if !bytes.Equal(fake.Invocations()[0].Stdin, []byte{'r', 'a', 'w', '\r', '\n', 0, 0xff}) {
		t.Fatal("recorded stdin aliases caller memory")
	}
}

func TestFakeAdapterReturnsErrorAndHonorsCancellation(t *testing.T) {
	want := errors.New("failed")
	if _, err := (&FakeAdapter{Err: want}).Run(context.Background(), Invocation{Stdin: bytes.NewReader(nil)}); !errors.Is(err, want) {
		t.Fatalf("error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := (&FakeAdapter{}).Run(ctx, Invocation{Stdin: bytes.NewReader(nil)}); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled error = %v", err)
	}
}
