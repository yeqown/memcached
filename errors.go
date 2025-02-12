package memcached

import (
	"github.com/pkg/errors"
)

var (
	// ErrNonexistentCommand response by server "ERROR"
	ErrNonexistentCommand = errors.New("nonexistent command")
	// ErrClientError response by server "CLIENT_ERROR <message>"
	ErrClientError = errors.New("client error")
	// ErrServerError response by server "SERVER_ERROR <message>"
	ErrServerError = errors.New("server error")
	// ErrNotFound response by server "NOT_FOUND"
	ErrNotFound = errors.New("not found")
	// ErrExists response by server "EXISTS"
	ErrExists = errors.New("exists")
	// ErrNotStored response by server "NOT_STORED"
	ErrNotStored = errors.New("not stored")

	// ErrMalformedResponse represents a malformed response error, it could be returned
	// when the response is not expected. Debug the server response to see whether it is
	// correct, if it is correct, please report this issue.
	ErrMalformedResponse = errors.New("malformed response")
	// ErrUnknownIndicator internal error represents an unknown indicator, please report this issue.
	ErrUnknownIndicator = errors.New("unknown indicator")
	// ErrInvalidAddress represents an invalid address error.
	// It is returned when the given address is invalid.
	ErrInvalidAddress = errors.New("invalid address")
)
