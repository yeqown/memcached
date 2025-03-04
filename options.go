package memcached

import (
	"time"
)

// ClientOption is the option set pattern for the client.
type ClientOption func(*clientOptions)

type clientOptions struct {
	pickBuilder Builder

	// resolver is the resolver for the client to resolve the given address
	// to a list of Addr. It supports both single address and cluster address.
	// .e.g.
	//  1. single address: IP_ADDRESS:11211
	// 	2. cluster address: IP_ADDRESS:11211,IP_ADDRESS:11212,IP_ADDRESS_ADDRESS:11211
	//
	// The defaultResolver supports tcp、udp and unix domain socket. Default is tcp if the address
	// is not specified start with `udp://` or `unix://`.
	resolver Resolver

	// dialTimeout is the timeout for dialing a connection to the memcached server
	// instance. Default is 5 seconds.
	// (Connection Timeout)
	dialTimeout time.Duration
	// readTimeout is the timeout for reading from the connection.
	// Default is 5 seconds.
	// (Connection Timeout)
	readTimeout time.Duration
	// writeTimeout is the timeout for writing to the connection.
	// Default is 5 seconds.
	// (Connection Timeout)
	writeTimeout time.Duration

	// maxConns is the max connections in the pool.
	// Default is 100.
	maxConns int
	// maxIdleConns is the max idle connections in the pool.
	// Default is 10.
	maxIdleConns int
	// maxLifetime is the max lifetime for a connection, 0 means no lifetime limit.
	// Default is 0.
	maxLifetime time.Duration
	// maxIdleTimeout is the max idle timeout for a connection, 0 means no idle timeout.
	// Default is 0.
	maxIdleTimeout time.Duration

	// noReply is the flag to indicate whether the client should wait for the response.
	noReply bool

	// enableTLS means whether the client should use TLS to connect to the server.
	enableSASL    bool
	plainUsername string
	plainPassword string

	// enableUDP means whether the client should use UDP datagram to send the request.
	enableUDP bool
}

func newClientOptions() *clientOptions {
	return &clientOptions{
		pickBuilder: crc32HashPickBuilder{},
		resolver:    defaultResolver{},

		dialTimeout:  3 * time.Second,
		readTimeout:  5 * time.Second,
		writeTimeout: 5 * time.Second,

		maxConns:       100,
		maxIdleConns:   10,
		maxLifetime:    0,
		maxIdleTimeout: 0,

		noReply: false,

		enableSASL:    false,
		plainUsername: "",
		plainPassword: "",
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

// WithWriteTimeout sets the write timeout for the client.
func WithWriteTimeout(timeout time.Duration) ClientOption {
	return func(o *clientOptions) {
		if timeout <= 0 {
			timeout = 5 * time.Second
		}

		o.writeTimeout = timeout
	}
}

// WithMaxConns sets the max connections in the pool.
func WithMaxConns(n int) ClientOption {
	return func(o *clientOptions) {
		if n <= 0 {
			n = 100
		}

		o.maxConns = n
	}
}

// WithMaxIdleConns sets the max idle connections in the pool.
func WithMaxIdleConns(n int) ClientOption {
	return func(o *clientOptions) {
		if n <= 0 {
			n = 10
		}

		o.maxIdleConns = n
	}
}

// WithMaxLifetime sets the max lifetime for a connection, 0 means no lifetime limit.
func WithMaxLifetime(d time.Duration) ClientOption {
	return func(o *clientOptions) {
		o.maxLifetime = d
	}
}

// WithMaxIdleTimeout sets the max idle timeout for a connection, 0 means no idle timeout.
func WithMaxIdleTimeout(d time.Duration) ClientOption {
	return func(o *clientOptions) {
		o.maxIdleTimeout = d
	}
}

// WithNoReply sets the flag to indicate whether the client should wait for the response.
func WithNoReply() ClientOption {
	return func(o *clientOptions) {
		o.noReply = true
	}
}

// WithSASL sets the SASL authentication for the client.
// @Deprecated: since SASL is supported over binary protocol, but binary protocol is deprecated.
func WithSASL(username, password string) ClientOption {
	return func(o *clientOptions) {
		o.enableSASL = true
		o.plainUsername = username
		o.plainPassword = password
	}
}

// WithUDPEnabled sets the UDP mode for the client.
// Note: UDP mode would affect all connections to all servers, NOT ONLY the udp servers.
func WithUDPEnabled() ClientOption {
	return func(o *clientOptions) {
		o.enableUDP = true
	}
}
