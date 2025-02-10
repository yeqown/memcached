package memcached

import (
	"github.com/pkg/errors"
)

var (
	ErrNonexistentCommand = errors.New("nonexistent command")
	ErrClientError        = errors.New("client error")
	ErrServerError        = errors.New("server error")
	ErrNotFound           = errors.New("not found")
	ErrExists             = errors.New("exists")
	ErrNotStored          = errors.New("not stored")

	ErrMalformedResponse = errors.New("malformed response")
	ErrUnknownIndicator  = errors.New("unknown indicator")
	ErrInvalidAddress    = errors.New("invalid address")
)
