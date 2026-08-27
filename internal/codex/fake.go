package codex

import (
	"context"
	"errors"
	"io"
	"sync"
)

// RecordedInvocation is a byte-for-byte record of one fake adapter call.
type RecordedInvocation struct {
	Workspace string
	Stdin     []byte
}

// FakeAdapter is deterministic and supports concurrent Run and Invocations
// calls when Response and Err are treated as immutable configuration.
type FakeAdapter struct {
	Response []byte
	Err      error

	mu          sync.Mutex
	invocations []RecordedInvocation
}

func (f *FakeAdapter) Run(ctx context.Context, invocation Invocation) (Result, error) {
	if invocation.Stdin == nil {
		return Result{}, errors.New("codex stdin is required")
	}
	select {
	case <-ctx.Done():
		return Result{}, ctx.Err()
	default:
	}

	stdin, err := io.ReadAll(invocation.Stdin)
	if err != nil {
		return Result{}, err
	}

	f.mu.Lock()
	f.invocations = append(f.invocations, RecordedInvocation{
		Workspace: invocation.Workspace,
		Stdin:     append([]byte(nil), stdin...),
	})
	response := append([]byte(nil), f.Response...)
	runErr := f.Err
	f.mu.Unlock()

	select {
	case <-ctx.Done():
		return Result{}, ctx.Err()
	default:
	}
	if runErr != nil {
		return Result{}, runErr
	}
	return Result{FinalMessage: response}, nil
}

// Invocations returns a deep copy of all calls recorded so far.
func (f *FakeAdapter) Invocations() []RecordedInvocation {
	f.mu.Lock()
	defer f.mu.Unlock()

	result := make([]RecordedInvocation, len(f.invocations))
	for i, invocation := range f.invocations {
		result[i] = RecordedInvocation{
			Workspace: invocation.Workspace,
			Stdin:     append([]byte(nil), invocation.Stdin...),
		}
	}
	return result
}
