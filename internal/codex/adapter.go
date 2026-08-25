// Package codex defines the provider-side Codex process boundary.
package codex

import (
	"context"
	"io"

	protocolv1 "github.com/wWzZb/peercontext/pkg/protocol/v1"
)

// Invocation contains only infrastructure inputs. Prompt interpretation and
// rewriting do not belong in the adapter or CLI.
type Invocation struct {
	Workspace string
	Mode      protocolv1.RequestMode
	Stdin     io.Reader
}

// Result contains the exact final Agent message bytes.
type Result struct {
	FinalMessage []byte
}

// Adapter runs one Codex request. The real isolated_runtime implementation is
// added with the read vertical slice; regular tests use FakeAdapter.
type Adapter interface {
	Run(context.Context, Invocation) (Result, error)
}
