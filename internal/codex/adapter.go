// Package codex defines the provider-side Codex process boundary.
package codex

import (
	"context"
	"io"
)

// Invocation contains only infrastructure inputs. Prompt interpretation and
// rewriting do not belong in the adapter or CLI.
type Invocation struct {
	Workspace string
	Stdin     io.Reader
}

// Result contains the exact final Agent message bytes.
type Result struct {
	FinalMessage []byte
}

// Adapter runs one read-only Codex request in isolated_runtime.
type Adapter interface {
	Run(context.Context, Invocation) (Result, error)
}
