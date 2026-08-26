// Package clioutput defines peerctx's public JSON envelopes and exit codes.
package clioutput

import (
	"encoding/json"
	"io"
)

// ExitCode is a stable peerctx process exit code.
type ExitCode int

const (
	ExitOK                   ExitCode = 0
	ExitInternal             ExitCode = 1
	ExitUsage                ExitCode = 2
	ExitConfiguration        ExitCode = 3
	ExitAuthentication       ExitCode = 4
	ExitAuthorization        ExitCode = 5
	ExitConnection           ExitCode = 6
	ExitNotFound             ExitCode = 7
	ExitConflict             ExitCode = 8
	ExitUnavailable          ExitCode = 9
	ExitConfirmationRequired ExitCode = 10
	ExitDenied               ExitCode = 11
	ExitTimeout              ExitCode = 12
	ExitCanceled             ExitCode = 13
	ExitProtocol             ExitCode = 14
	ExitRuntime              ExitCode = 15
)

// Meta accompanies successful output. RequestID is omitted for commands that
// do not create or inspect a request.
type Meta struct {
	RequestID string `json:"request_id,omitempty"`
	Version   string `json:"version"`
}

// SuccessEnvelope is the only top-level shape written to stdout on success.
type SuccessEnvelope struct {
	OK   bool `json:"ok"`
	Data any  `json:"data"`
	Meta Meta `json:"meta"`
}

// Error is the stable, machine-readable failure payload.
type Error struct {
	Type      string `json:"type"`
	Subtype   string `json:"subtype"`
	Code      string `json:"code"`
	Message   string `json:"message"`
	Hint      string `json:"hint"`
	Retryable bool   `json:"retryable"`
	Details   any    `json:"details,omitempty"`
	exitCode  ExitCode
}

// WithDetails attaches structured machine data to a non-success state such as
// the mandatory requester write confirmation. It never changes the exit code.
func (e Error) WithDetails(details any) Error {
	e.Details = details
	return e
}

// ErrorEnvelope is the only top-level shape written to stderr on failure.
type ErrorEnvelope struct {
	OK    bool  `json:"ok"`
	Error Error `json:"error"`
}

// NewError constructs a structured error and binds it to a stable exit code.
func NewError(exitCode ExitCode, errorType, subtype, code, message, hint string, retryable bool) Error {
	return Error{
		Type:      errorType,
		Subtype:   subtype,
		Code:      code,
		Message:   message,
		Hint:      hint,
		Retryable: retryable,
		exitCode:  exitCode,
	}
}

// ExitCode returns the process exit code associated with the error.
func (e Error) ExitCode() ExitCode {
	if e.exitCode == ExitOK {
		return ExitInternal
	}
	return e.exitCode
}

// WriteSuccess writes exactly one JSON object followed by a newline.
func WriteSuccess(w io.Writer, data any, meta Meta) error {
	return writeJSON(w, SuccessEnvelope{OK: true, Data: data, Meta: meta})
}

// WriteError writes exactly one JSON object followed by a newline.
func WriteError(w io.Writer, apiErr Error) error {
	return writeJSON(w, ErrorEnvelope{OK: false, Error: apiErr})
}

func writeJSON(w io.Writer, value any) error {
	encoder := json.NewEncoder(w)
	encoder.SetEscapeHTML(false)
	return encoder.Encode(value)
}
