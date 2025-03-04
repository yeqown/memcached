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
	// ErrAuthenticationUnSupported represents an authentication not supported error.
	// no need to authenticate or the server does not support PLAIN mechanism.
	ErrAuthenticationUnSupported = errors.New("authentication not supported")
	// ErrAuthenticationFailed represents an authentication failed error.
	ErrAuthenticationFailed = errors.New("authentication failed")
	// ErrInvalidArgument represents an invalid arguments error.
	ErrInvalidArgument = errors.New("invalid arguments")
	// ErrNotSupported represents a not supported error.
	ErrNotSupported = errors.New("not supported")

	// ErrMalformedResponse represents a malformed response error, it could be returned
	// when the response is not expected. Debug the server response to see whether it is
	// correct, if it is correct, please report this issue.
	ErrMalformedResponse = errors.New("malformed response")
	// ErrUnknownIndicator internal error represents an unknown indicator, please report this issue.
	ErrUnknownIndicator = errors.New("unknown indicator")
	// ErrInvalidAddress represents an invalid address error.
	// It is returned when the given address is invalid.
	ErrInvalidAddress = errors.New("invalid address")
	// ErrInvalidKey represents an invalid key error.
	ErrInvalidKey = errors.New("invalid key empty or too long(over than 2^16)")
	// ErrInvalidValue represents an invalid value error.
	ErrInvalidValue = errors.New("invalid value too long(over than 2^32)")
	// ErrInvalidBinaryProtocol represents an invalid binary protocol error.
	ErrInvalidBinaryProtocol = errors.New("invalid binary protocol")
	// ErrInvalidNetworkProtocol represents an invalid network protocol error.
	ErrInvalidNetworkProtocol = errors.New("invalid network protocol")
)
