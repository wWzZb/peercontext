// Package failure carries stable PeerContext errors across internal process and
// network boundaries without relying on matching human-readable text.
package failure

import (
	"context"
	"errors"
)

// Error is a safe, structured failure. Cause is intentionally excluded from
// JSON so local implementation details do not cross the control socket.
type Error struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Retryable bool   `json:"retryable"`
	Cause     error  `json:"-"`
}

func (e *Error) Error() string {
	if e == nil {
		return "PeerContext operation failed"
	}
	return e.Message
}

func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

func New(code, message string, retryable bool) *Error {
	return &Error{Code: code, Message: message, Retryable: retryable}
}

func Wrap(code, message string, retryable bool, cause error) *Error {
	return &Error{Code: code, Message: message, Retryable: retryable, Cause: cause}
}

// Normalize preserves an existing structured error and otherwise returns a
// generic safe error for the local control protocol.
func Normalize(err error) *Error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return New("request_timeout", "The PeerContext request timed out.", true)
	}
	if errors.Is(err, context.Canceled) {
		return New("request_canceled", "The PeerContext request was canceled.", false)
	}
	var structured *Error
	if errors.As(err, &structured) {
		return &Error{Code: structured.Code, Message: structured.Message, Retryable: structured.Retryable}
	}
	return New("peerctx_error", err.Error(), false)
}
