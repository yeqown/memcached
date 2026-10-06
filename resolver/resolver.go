// Package resolver resolves targets into complete Memcached node topologies
// and decides when the client should resolve them again.
package resolver

import (
	"context"
	"time"
)

// Resolver resolves a target to a complete list of data nodes. It must honor ctx.
// A nil nextResolveAt stops refreshing, including on error; transient failures
// should return a retry time. Scheduling, backoff and source version validation
// belong to Resolver. Returned addresses must not be modified concurrently with
// their delivery; metadata values must be immutable. If a Resolver implements
// io.Closer, Client owns its lifetime and closes it when Client closes or its
// initialization fails. Such a Resolver must be dedicated to one client.
// Other stateful resolvers must synchronize their state if shared.
type Resolver interface {
	Resolve(ctx context.Context, target string) (result ResolveResult, nextResolveAt *time.Time, err error)
}

// ResolveResult is a complete set of data nodes obtained from a target.
// Generation is optional source information, and may be used to avoid applying unchanged topologies.
type ResolveResult struct {
	Addrs      []*Addr
	Generation uint64
}
