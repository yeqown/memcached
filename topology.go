package memcached

import (
	"context"
	"log"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/hashicorp/go-multierror"
	pkgerrors "github.com/pkg/errors"

	"github.com/yeqown/memcached/picker"
	"github.com/yeqown/memcached/resolver"
	"github.com/yeqown/memcached/telemetry"
)

// topology owns discovery and node membership. Routing uses cached addresses;
// each node owns borrowing, returning and draining its connections.
type topology struct {
	target   string
	makeNode func(*resolver.Addr) *node

	resolveTimeout time.Duration
	resolver       resolver.Resolver
	metrics        *telemetry.Metrics

	mu          sync.RWMutex
	cachedAddrs []*resolver.Addr // immutable until replaced by an update
	nodes       map[resolver.AddrKey]*node
	generation  uint64
	closed      atomic.Bool
}

func newTopology(
	ctx context.Context,
	target string,
	r resolver.Resolver,
	resolveTimeout time.Duration,
	makeNode func(*resolver.Addr) *node,
	metrics *telemetry.Metrics,
) (*topology, error) {
	lifetimeCtx := context.Background()
	t := &topology{
		target:   target,
		makeNode: makeNode,

		resolveTimeout: resolveTimeout,
		resolver:       r,
		metrics:        metrics,

		mu:          sync.RWMutex{},
		cachedAddrs: nil,
		nodes:       make(map[resolver.AddrKey]*node),
		generation:  0,
		closed:      atomic.Bool{},
	}

	resolveCtx, cancel := context.WithTimeout(ctx, resolveTimeout)
	defer cancel()
	nextAt, err := t.resolve(resolveCtx)
	if err != nil {
		_ = t.close()
		// Preserve the initialization error.
		return nil, pkgerrors.Wrap(err, "resolve failed")
	}

	if nextAt != nil {
		go t.resolverLoop(lifetimeCtx, nextAt)
	}

	return t, nil
}

// pickNode keeps routing and lookup on the same membership under a read lock.
// The caller borrows after unlocking; removal may then reject the borrow.
func (t *topology) pickNode(p picker.Picker, cmd, key []byte) (*node, error) {
	if t.closed.Load() {
		return nil, ErrClientClosed
	}

	t.mu.RLock()
	defer t.mu.RUnlock()

	addr, err := p.Pick(t.cachedAddrs, cmd, key)
	if err != nil {
		return nil, pkgerrors.Wrap(err, "pick node failed")
	}
	if addr == nil {
		return nil, pkgerrors.Wrap(ErrInvalidAddress, "nil picker address")
	}
	n := t.nodes[addr.AddrKey]
	if n == nil {
		return nil, pkgerrors.Wrap(ErrInvalidAddress, "picker selected a node outside the available addresses")
	}
	return n, nil
}

// allNodes captures one broadcast's targets. It neither borrows nor executes.
func (t *topology) allNodes() ([]*node, error) {
	if t.closed.Load() {
		return nil, ErrClientClosed
	}

	t.mu.RLock()
	defer t.mu.RUnlock()
	nodes := make([]*node, 0, len(t.nodes))
	for _, addr := range t.cachedAddrs {
		nodes = append(nodes, t.nodes[addr.AddrKey])
	}
	return nodes, nil
}

func (t *topology) apply(ctx context.Context, result resolver.ResolveResult) error {
	if t.closed.Load() {
		return ErrClientClosed
	}

	// if err := ctx.Err(); err != nil {
	// 	return err
	// }

	t.mu.Lock()
	if t.closed.Load() {
		t.mu.Unlock()
		return ErrClientClosed
	}
	if len(result.Addrs) == 0 {
		t.mu.Unlock()
		return pkgerrors.Wrap(ErrInvalidAddress, "empty topology")
	}
	if result.Generation != 0 && result.Generation == t.generation {
		t.mu.Unlock()
		return nil
	}

	// Resolver validates and canonicalizes addresses. Check the membership's
	// structure before creating nodes, then take ownership of routing inputs.
	addrs := make([]*resolver.Addr, 0, len(result.Addrs))
	seen := make(map[resolver.AddrKey]struct{}, len(result.Addrs))
	for _, addr := range result.Addrs {
		if addr == nil {
			t.mu.Unlock()
			return pkgerrors.Wrap(ErrInvalidAddress, "nil topology node")
		}
		if _, duplicate := seen[addr.AddrKey]; duplicate {
			t.mu.Unlock()
			return pkgerrors.Wrap(ErrInvalidAddress, "duplicate topology node")
		}
		seen[addr.AddrKey] = struct{}{}
		addrs = append(addrs, addr.Clone())
	}
	// Keep modulo routing stable when a resolver changes only response order.
	slices.SortFunc(addrs, func(a, b *resolver.Addr) int {
		if cmp := strings.Compare(a.Network, b.Network); cmp != 0 {
			return cmp
		}
		return strings.Compare(a.Address, b.Address)
	})
	nodes := make(map[resolver.AddrKey]*node, len(addrs))
	for _, addr := range addrs {
		n := t.nodes[addr.AddrKey]
		if n == nil {
			n = t.makeNode(addr)
		}
		nodes[addr.AddrKey] = n
	}
	var removed []*node
	for key, n := range t.nodes {
		if nodes[key] == nil {
			removed = append(removed, n)
		}
	}
	t.cachedAddrs, t.nodes, t.generation = addrs, nodes, result.Generation
	t.metrics.RecordTopology(ctx, len(addrs), result.Generation)
	t.mu.Unlock()

	for _, n := range removed {
		_ = n.close() // Discovery has no caller to receive retirement errors.
	}
	return nil
}

func (t *topology) close() error {
	if swapped := t.closed.CompareAndSwap(false, true); !swapped {
		return nil
	}

	var err error

	if re := t.resolver.Close(); re != nil {
		err = multierror.Append(err, pkgerrors.Wrap(re, "resolver close"))
	}

	t.mu.RLock()
	nodes := make([]*node, 0, len(t.nodes))
	for _, n := range t.nodes {
		nodes = append(nodes, n)
	}
	t.mu.RUnlock()

	for _, n := range nodes {
		if ne := n.close(); ne != nil {
			err = multierror.Append(err, pkgerrors.Wrap(ne, "node close"))
		}
	}

	return err
}

// resolve runs one discovery attempt. Resolver may recover the attempt timeout
// with a cached result; apply publishes successful results unless already closed.
func (t *topology) resolve(ctx context.Context) (nextAt *time.Time, err error) {
	start := time.Now()
	defer func() {
		t.metrics.RecordResolve(ctx, time.Since(start), err)
	}()

	result, next, err := t.resolver.Resolve(ctx, t.target)
	if next != nil {
		copyTime := *next
		nextAt = &copyTime
	}
	if err != nil {
		return nextAt, pkgerrors.Wrap(err, "resolver resolve failed")
	}

	if err = t.apply(ctx, result); err != nil {
		return nextAt, pkgerrors.Wrap(err, "topology apply failed")
	}

	return nextAt, err
}

func (t *topology) resolverLoop(ctx context.Context, nextAt *time.Time) {
	var err error
	for nextAt != nil {
		timer := time.NewTimer(max(0, time.Until(*nextAt)))
		select {
		case <-ctx.Done():
			timer.Stop()
			log.Printf("topology: resolver loop exiting: %v", ctx.Err())
			return
		case <-timer.C:
		}

		resolveCtx, cancel := context.WithTimeout(ctx, t.resolveTimeout)
		if nextAt, err = t.resolve(resolveCtx); err != nil {
			log.Printf("topology: resolve failed: %v", err)
		}
		cancel()
	}
}
