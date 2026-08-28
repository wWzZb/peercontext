package v2

import "errors"

var (
	ErrInvalidInvitation = errors.New("invalid invitation")
	ErrInvitationExpired = errors.New("invitation expired")
	ErrClockSkew         = errors.New("clock skew")
	ErrSignatureInvalid  = errors.New("signature invalid")
)
