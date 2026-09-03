package control

import "errors"

// Sentinel errors mapped to API codes by the transport layers.
var (
	// ErrInvalidToken covers unknown, used, expired and malformed tokens
	// with one indistinguishable message.
	ErrInvalidToken = errors.New("invalid token")
	// ErrValidation marks a rejected input; the message names the field.
	ErrValidation = errors.New("validation failed")
	// ErrForbidden marks an action the principal may not perform.
	ErrForbidden = errors.New("forbidden")
	// ErrRateLimited marks too many attempts from one source.
	ErrRateLimited = errors.New("too many attempts")
	// ErrVersion marks a client older than min_client_version.
	ErrVersion = errors.New("client version too old")
	// ErrNotFound wraps store misses so callers need not import store.
	ErrNotFound = errors.New("not found")
)
