// Package resolver resolves targets into complete Memcached node topologies
// and decides when the client should resolve them again.
package resolver

import (
	"context"
	"io"
	"slices"
	"strings"
	"time"
)

// Resolver resolves a target to a complete list of data nodes. It must honor ctx,
// but may recover a timeout by returning a complete cached result and nil error.
// Explicit cancellation must still be returned as an error.
// A nil nextResolveAt stops refreshing, including on error; transient failures
// should return a retry time. Scheduling, backoff and source version validation
// belong to Resolver. Returned addresses must not be modified concurrently with
// their delivery. Resolvers must validate address syntax and return unique
// network/address string identities. Resolvers control address order; topology
// preserves the supplied order, which affects modulo routing.
// Client owns the Resolver's lifetime and calls Close when Client closes or
// initialization fails. Each Resolver must be dedicated to one client.
type Resolver interface {
	io.Closer

	Resolve(ctx context.Context, target string) (result ResolveResult, nextResolveAt *time.Time, err error)
}

// ResolveResult is a complete set of data nodes obtained from a target.
type ResolveResult struct {
	// Addrs is the complete, nonempty node list for a successful resolution.
	// An empty list is invalid; topology rejects it and retains existing nodes.
	Addrs []*Addr

	// Generation is an optional source version scoped to the target.
	// Equal nonzero generations must identify identical ordered address lists.
	// Any change to that content must use a different generation; topology may
	// skip applying a result with the same nonzero generation.
	// Zero means unspecified; topology applies the result without version skipping.
	Generation uint64
}

func (r ResolveResult) Clone() ResolveResult {
	cloned := ResolveResult{Generation: r.Generation, Addrs: make([]*Addr, len(r.Addrs))}
	for i, addr := range r.Addrs {
		cloned.Addrs[i] = addr.Clone()
	}

	return cloned
}

// SortAddrs sorts addresses by network and address before publication or caching.
func (r ResolveResult) SortAddrs() {
	if len(r.Addrs) <= 1 {
		return
	}

	// Keep modulo routing stable when a resolver changes only response order.
	slices.SortFunc(r.Addrs, func(a, b *Addr) int {
		if cmp := strings.Compare(a.Network, b.Network); cmp != 0 {
			return cmp
		}

		return strings.Compare(a.Address, b.Address)
	})
}
