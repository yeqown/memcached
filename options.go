package memcached

import (
	"time"
)

type ClientOption func(*clientOptions)

type clientOptions struct {
	pickBuilder Builder
	resolver    Resolver

	// dialTimeout is the timeout for dialing a connection to the memcached server
	// instance. Default is 5 seconds.
	dialTimeout time.Duration

	// readTimeout is the timeout for reading from the connection.
	// Default is 5 seconds.
	readTimeout time.Duration

	// writeTimeout is the timeout for writing to the connection.
	// Default is 5 seconds.
	writeTimeout time.Duration

	// noReply is the flag to indicate whether the client should wait for the response.
	noReply bool
}

func newClientOptions() *clientOptions {
	return &clientOptions{
		pickBuilder: crc32HashPickBuilder{},
		resolver:    defaultResolver{},

		dialTimeout:  5 * time.Second,
		readTimeout:  5 * time.Second,
		writeTimeout: 5 * time.Second,
	}
}

// WithResolver sets the resolver for the client to resolve the given address
// to a list of Addr.
func WithResolver(r Resolver) ClientOption {
	return func(o *clientOptions) {
		if r == nil {
			return
		}

		o.resolver = r
	}
}

// WithPickBuilder sets the pickBuilder for the client to build a Picker from
// a list of Addr.
func WithPickBuilder(p Builder) ClientOption {
	return func(o *clientOptions) {
		if p == nil {
			return
		}

		o.pickBuilder = p
	}
}

// WithDialTimeout sets the dial timeout for the client.
// Default is 5 seconds.
func WithDialTimeout(timeout time.Duration) ClientOption {
	return func(o *clientOptions) {
		if timeout <= 0 {
			timeout = 5 * time.Second
		}

		o.dialTimeout = timeout
	}
}

// WithReadTimeout sets the read timeout for the client.
// Default is 5 seconds.
func WithReadTimeout(timeout time.Duration) ClientOption {
	return func(o *clientOptions) {
		if timeout <= 0 {
			timeout = 5 * time.Second
		}

		o.readTimeout = timeout
	}
}

func WithWriteTimeout(timeout time.Duration) ClientOption {
	return func(o *clientOptions) {
		if timeout <= 0 {
			timeout = 5 * time.Second
		}

		o.writeTimeout = timeout
	}
}

// WithNoReply sets the flag to indicate whether the client should wait for the response.
func WithNoReply() ClientOption {
	return func(o *clientOptions) {
		o.noReply = true
	}
}
