package memcached

import (
	"github.com/pkg/errors"
	"github.com/yeqown/memcached/resolver"
)

// Client and node state.
var (
	// ErrClientClosed is returned when a client or its connection pool is closed.
	ErrClientClosed = errors.New("client is closed")
	// ErrInstanceAbnormal is returned when a node no longer accepts new requests.
	// The name is retained for API compatibility.
	ErrInstanceAbnormal = errors.New("instance is abnormal")
)

// Argument and address validation.
var (
	// ErrInvalidAddress represents an invalid address error.
	// It is returned when the given address is invalid.
	ErrInvalidAddress = resolver.ErrInvalidAddress
	// ErrInvalidArgument represents an invalid arguments error.
	ErrInvalidArgument = errors.New("invalid arguments")
	// ErrInvalidKey represents an invalid key error.
	ErrInvalidKey = errors.New("invalid key empty or too long(over than 2^16)")
	// ErrInvalidNetworkProtocol represents an invalid network protocol error.
	ErrInvalidNetworkProtocol = resolver.ErrInvalidNetworkProtocol
	// ErrInvalidValue represents an invalid value error.
	ErrInvalidValue = errors.New("invalid value too long(over than 2^32)")
)

// Authentication.
var (
	// ErrAuthenticationFailed represents an authentication failed error.
	ErrAuthenticationFailed = errors.New("authentication failed")
	// ErrAuthenticationUnSupported represents an authentication not supported error.
	// no need to authenticate or the server does not support PLAIN mechanism.
	ErrAuthenticationUnSupported = errors.New("authentication not supported")
)

// Protocol support and response parsing.
var (
	// ErrInvalidBinaryProtocol represents an invalid binary protocol error.
	ErrInvalidBinaryProtocol = errors.New("invalid binary protocol")
	// ErrMalformedResponse represents a malformed response error, it could be returned
	// when the response is not expected. Debug the server response to see whether it is
	// correct, if it is correct, please report this issue.
	ErrMalformedResponse = errors.New("malformed response")
	// ErrNotSupported represents a not supported error.
	ErrNotSupported = errors.New("not supported")
	// ErrUnknownIndicator internal error represents an unknown indicator, please report this issue.
	ErrUnknownIndicator = errors.New("unknown indicator")
)

// Memcached server responses.
var (
	// ErrClientError response by server "CLIENT_ERROR <message>"
	ErrClientError = errors.New("client error")
	// ErrExists response by server "EXISTS"
	ErrExists = errors.New("exists")
	// ErrNonexistentCommand response by server "ERROR"
	ErrNonexistentCommand = errors.New("nonexistent command")
	// ErrNotFound response by server "NOT_FOUND"
	ErrNotFound = errors.New("not found")
	// ErrNotStored response by server "NOT_STORED"
	ErrNotStored = errors.New("not stored")
	// ErrServerError response by server "SERVER_ERROR <message>"
	ErrServerError = errors.New("server error")
)
