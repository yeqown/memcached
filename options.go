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
}

func newClientOptions() *clientOptions {
	return &clientOptions{
		pickBuilder: randomPickBuilder{},
		resolver:    defaultResolver{},

		dialTimeout:  5 * time.Second,
		readTimeout:  5 * time.Second,
		writeTimeout: 5 * time.Second,
	}
}
