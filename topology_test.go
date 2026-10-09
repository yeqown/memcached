package memcached

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/yeqown/memcached/internal/testutil"
	"github.com/yeqown/memcached/picker"
	"github.com/yeqown/memcached/resolver"
	"github.com/yeqown/memcached/telemetry"
)

func TestTopologyUpdatesMembership(t *testing.T) {
	a, b, d := resolver.NewAddr("tcp", "a:11211", 0), resolver.NewAddr("tcp", "b:11211", 0), resolver.NewAddr("tcp", "d:11211", 0)
	for _, test := range []struct {
		name    string
		next    []*resolver.Addr
		removed bool
	}{
		{"same identities", []*resolver.Addr{a.Clone(), b.Clone()}, false},
		{"reordered", []*resolver.Addr{b, a}, false},
		{"addition", []*resolver.Addr{a, b, d}, false},
		{"removal", []*resolver.Addr{b}, true},
		{"replacement", []*resolver.Addr{b, d}, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			top := newTestTopology(t, []*resolver.Addr{a, b})
			old := topologyView(top)
			require.NoError(t, applyTopology(top, test.next))
			current := topologyView(top)
			require.Len(t, current.nodes, len(test.next))
			require.ElementsMatch(t, test.next, current.addrs, "published routing addresses must match the resolved membership")
			require.Same(t, old.nodes[b.AddrKey], current.nodes[b.AddrKey])
			if test.removed {
				require.NotContains(t, current.nodes, a.AddrKey)
				require.Equal(t, nodeClosed, old.nodes[a.AddrKey].status())
			} else {
				require.Same(t, old.nodes[a.AddrKey], current.nodes[a.AddrKey])
			}
		})
	}
}

func TestTopologyUpdatesRoutingInputsForSameNode(t *testing.T) {
	addr := resolver.NewAddr("tcp", "cache.example:11211", 1)
	addr.Add("zone", "one")
	top := newTestTopology(t, []*resolver.Addr{addr})
	initial := topologyView(top)

	updated := resolver.NewAddr("tcp", "cache.example:11211", 2)
	updated.Add("zone", "two")
	require.True(t, addr.AddrKey.Equal(updated.AddrKey), "priority and metadata do not change node identity")

	require.NoError(t, applyTopology(top, []*resolver.Addr{updated}))
	current := topologyView(top)
	require.Equal(t, 2, current.addrs[0].Priority)
	require.Equal(t, "two", current.addrs[0].GetMetadata("zone"))
	id := addr.AddrKey
	require.Same(t, initial.nodes[id], current.nodes[id], "the same node keeps its connection pool")
}

func TestTopologyOwnsRoutingInputs(t *testing.T) {
	a := resolver.NewAddr("tcp", "cache.example:11211", 1)
	a.Add("zone", "one")
	top := newTestTopology(t, []*resolver.Addr{resolver.NewAddr("tcp", "other:11211", 0)})
	require.NoError(t, applyTopology(top, []*resolver.Addr{a}))
	a.Address, a.Priority = "changed:11211", 9
	a.Add("zone", "changed")
	view := topologyView(top)
	require.Equal(t, "cache.example:11211", view.addrs[0].Address)
	require.Equal(t, 1, view.addrs[0].Priority)
	require.Equal(t, "one", view.addrs[0].GetMetadata("zone"))
}

func TestTopologyInvalidRefreshPreservesNodes(t *testing.T) {
	a := resolver.NewAddr("tcp", "a:11211", 0)
	top := newTestTopology(t, []*resolver.Addr{a})
	before := topologyView(top)
	for _, addrs := range [][]*resolver.Addr{nil, {nil}, {a, a.Clone()}} {
		require.Error(t, applyTopology(top, addrs))
		require.Equal(t, before, topologyView(top))
	}
}

func TestTopologyCloseDrainsRemovedNode(t *testing.T) {
	top := newTestTopology(t, []*resolver.Addr{resolver.NewAddr("tcp", "a:11211", 0)})
	n, err := top.pickNode(picker.NewCRC32HashPicker(), nil, nil)
	require.NoError(t, err)
	raw := newTestConn()
	n.pool.createConn = func(context.Context) (memcachedConn, error) { return raw, nil }
	_, releaseFn, err := n.getConn(t.Context())
	require.NoError(t, err)
	defer releaseFn()
	require.NoError(t, applyTopology(top, []*resolver.Addr{resolver.NewAddr("tcp", "b:11211", 0)}))
	require.NoError(t, top.close())
	require.Equal(t, nodeDraining, n.status())
	require.Zero(t, raw.closes.Load(), "shutdown preserves the borrowed connection")
	releaseFn()
	requirePoolClosed(t, n.pool)
	require.EqualValues(t, 1, raw.closes.Load())
	require.Equal(t, nodeClosed, n.status())
}

func TestTopologySelectionAndLookupUseSameMembership(t *testing.T) {
	entered, proceed := make(chan struct{}), make(chan struct{})
	finish := sync.OnceFunc(func() { close(proceed) })
	defer finish()
	p := testPickerFunc(func(addrs []*resolver.Addr, _, _ []byte) (*resolver.Addr, error) {
		close(entered)
		<-proceed
		return addrs[0], nil
	})
	top := newTestTopology(t, []*resolver.Addr{resolver.NewAddr("tcp", "a:11211", 0)})
	raw := newTestConn()
	topologyView(top).nodes[resolver.AddrKey{Network: "tcp", Address: "a:11211"}].pool.createConn = func(context.Context) (memcachedConn, error) { return raw, nil }
	picked := make(chan struct{})
	var inst *node
	var pickErr error
	go func() {
		inst, pickErr = top.pickNode(p, nil, nil)
		close(picked)
	}()
	<-entered
	updated := make(chan error, 1)
	go func() {
		updated <- applyTopology(top, []*resolver.Addr{resolver.NewAddr("tcp", "b:11211", 0)})
	}()
	// Mutex waits are not durable in synctest; use a real bounded wait.
	select {
	case <-updated:
		t.Fatal("update split routing from node lookup")
	case <-time.After(20 * time.Millisecond):
	}
	finish()
	<-picked
	require.NoError(t, pickErr)
	require.NoError(t, <-updated)
	cn, releaseFn, err := inst.getConn(context.Background())
	require.ErrorIs(t, err, ErrInstanceAbnormal, "selection does not reserve a connection before removal")
	require.Nil(t, cn)
	require.Nil(t, releaseFn)
	requirePoolClosed(t, inst.pool)
}

func TestTopologyRemovalAndReaddUsesNewPool(t *testing.T) {
	a, b := resolver.NewAddr("tcp", "a:11211", 0), resolver.NewAddr("tcp", "b:11211", 0)
	top := newTestTopology(t, []*resolver.Addr{a})
	old := topologyView(top).nodes[a.AddrKey]
	raw := newTestConn()
	old.pool.createConn = func(context.Context) (memcachedConn, error) { return raw, nil }
	_, releaseOld, err := old.getConn(t.Context())
	require.NoError(t, err)
	defer releaseOld()
	require.NoError(t, applyTopology(top, []*resolver.Addr{b}))
	require.Equal(t, nodeDraining, old.status())
	cn, releaseFn, err := old.getConn(t.Context())
	require.ErrorIs(t, err, ErrInstanceAbnormal, "removal rejects a selected node before borrowing")
	require.Nil(t, cn)
	require.Nil(t, releaseFn)
	require.NoError(t, applyTopology(top, []*resolver.Addr{a, b}))
	current := topologyView(top).nodes[a.AddrKey]
	require.NotSame(t, old, current)
	require.NotSame(t, old.pool, current.pool)
	require.Zero(t, raw.closes.Load())
	releaseOld()
	require.Equal(t, nodeClosed, old.status())
	require.EqualValues(t, 1, raw.closes.Load())
	require.Equal(t, nodeAvailable, current.status())
}

func TestTopologyRemovalDoesNotWaitForOtherNodes(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		a, b := resolver.NewAddr("tcp", "a:11211", 0), resolver.NewAddr("tcp", "b:11211", 0)
		top := newTestTopology(t, []*resolver.Addr{a, b})
		initial := topologyView(top)
		raw := newTestConn()
		initial.nodes[b.AddrKey].pool.createConn = func(context.Context) (memcachedConn, error) { return raw, nil }
		_, release, err := initial.nodes[b.AddrKey].getConn(t.Context())
		require.NoError(t, err)
		defer release()
		done := make(chan error, 1)
		go func() { done <- applyTopology(top, []*resolver.Addr{b}) }()
		synctest.Wait()
		require.Len(t, done, 1, "a lease on a retained node must not delay removal")
		require.NoError(t, <-done)
		requirePoolClosed(t, initial.nodes[a.AddrKey].pool)
		require.Zero(t, raw.closes.Load())
		release()
		require.Equal(t, 1, raw.pool.stats().IdleConns)
	})
}

func TestTopologyResolverScheduleAndError(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var calls atomic.Int32
		r := resolverFunc(func(ctx context.Context, target string) (resolver.ResolveResult, *time.Time, error) {
			if target != "http://directory" {
				return resolver.ResolveResult{}, nil, fmt.Errorf("unexpected target %q", target)
			}
			if _, ok := ctx.Deadline(); !ok {
				return resolver.ResolveResult{}, nil, errors.New("missing resolve deadline")
			}
			call := calls.Add(1)
			next := time.Now().Add(time.Hour)
			switch call {
			case 1:
				return resolver.ResolveResult{Addrs: []*resolver.Addr{resolver.NewAddr("tcp", "a:11211", 0)}}, &next, nil
			case 2:
				return resolver.ResolveResult{}, &next, errors.New("directory unavailable")
			default:
				return resolver.ResolveResult{Addrs: []*resolver.Addr{resolver.NewAddr("tcp", "a:11211", 0)}}, nil, nil
			}
		})
		top, err := newTopology(t.Context(), "http://directory", r, 5*time.Second, func(addr *resolver.Addr) *node { return newTestNode(t, addr, 4) }, nil)
		require.NoError(t, err)
		defer func() { require.NoError(t, top.close()) }()
		synctest.Wait()
		require.Equal(t, int32(1), calls.Load())
		time.Sleep(time.Hour - time.Second)
		synctest.Wait()
		require.Equal(t, int32(1), calls.Load(), "resolution must wait for the supplied schedule")
		time.Sleep(time.Second)
		synctest.Wait()
		require.Equal(t, int32(2), calls.Load())
		time.Sleep(time.Hour)
		synctest.Wait()
		require.Equal(t, int32(3), calls.Load(), "an error with a retry schedule must continue discovery")
		time.Sleep(24 * time.Hour)
		synctest.Wait()
		require.Equal(t, int32(3), calls.Load(), "a nil schedule stops discovery")
	})
}

func TestTopologyResolverNilErrorScheduleStopsRefresh(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		calls := 0
		r := resolverFunc(func(context.Context, string) (resolver.ResolveResult, *time.Time, error) {
			calls++
			if calls == 1 {
				next := time.Now().Add(time.Minute)
				return resolver.ResolveResult{Addrs: []*resolver.Addr{resolver.NewAddr("tcp", "a:11211", 0)}}, &next, nil
			}
			return resolver.ResolveResult{}, nil, errors.New("stop refreshing")
		})
		top, err := newTopology(t.Context(), "directory", r, time.Second, func(addr *resolver.Addr) *node { return newTestNode(t, addr, 4) }, nil)
		require.NoError(t, err)
		defer func() { require.NoError(t, top.close()) }()
		time.Sleep(time.Minute)
		synctest.Wait()
		require.Equal(t, 2, calls)
		time.Sleep(time.Hour)
		synctest.Wait()
		require.Equal(t, 2, calls, "a nil schedule also stops discovery on error")
	})
}

func TestTopologyResolverTimeoutAndClose(t *testing.T) {
	for _, initial := range []bool{true, false} {
		t.Run(map[bool]string{true: "initial", false: "background"}[initial], func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				calls := 0
				resolveErrors := make(chan error, 1)
				r := resolverFunc(func(ctx context.Context, _ string) (resolver.ResolveResult, *time.Time, error) {
					calls++
					if initial || calls > 1 {
						<-ctx.Done()
						resolveErrors <- ctx.Err()
						return resolver.ResolveResult{}, nil, ctx.Err()
					}
					next := time.Now().Add(time.Minute)
					return resolver.ResolveResult{Addrs: []*resolver.Addr{resolver.NewAddr("tcp", "a:11211", 0)}}, &next, nil
				})
				top, err := newTopology(t.Context(), "directory", r, time.Second, func(addr *resolver.Addr) *node { return newTestNode(t, addr, 4) }, nil)
				if initial {
					require.ErrorIs(t, err, context.DeadlineExceeded)
					require.ErrorIs(t, <-resolveErrors, context.DeadlineExceeded)
					return
				}
				require.NoError(t, err)
				time.Sleep(time.Minute)
				synctest.Wait()
				require.NoError(t, top.close())
				require.Empty(t, resolveErrors, "closing a custom resolver does not cancel its attempt context")
				time.Sleep(time.Second)
				synctest.Wait()
				require.ErrorIs(t, <-resolveErrors, context.DeadlineExceeded)
				require.NoError(t, top.close())
				_, closedErr := top.allNodes()
				require.ErrorIs(t, closedErr, ErrClientClosed)
			})
		})
	}
}

func TestTopologyResolverCopiesReturnedSchedule(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		a := resolver.NewAddr("tcp", "a:11211", 0)
		top := newTestTopology(t, []*resolver.Addr{a})
		var calls atomic.Int32
		schedule := time.Now().Add(time.Hour)
		top.resolver = resolverFunc(func(context.Context, string) (resolver.ResolveResult, *time.Time, error) {
			call := calls.Add(1)
			result := resolver.ResolveResult{Generation: 1, Addrs: []*resolver.Addr{a}}
			if call == 1 {
				return result, &schedule, nil
			}
			return result, nil, nil
		})
		next, err := top.resolve(t.Context())
		require.NoError(t, err)
		require.NotNil(t, next)
		// Mutate before starting the loop so this ownership check has no data race.
		schedule = time.Now().Add(time.Minute)
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		go top.resolverLoop(ctx, next)
		time.Sleep(2 * time.Minute)
		synctest.Wait()
		require.Equal(t, 1, int(calls.Load()), "resolver-owned timestamp changes must not alter the captured schedule")
		time.Sleep(58 * time.Minute)
		synctest.Wait()
		require.Equal(t, 2, int(calls.Load()))
	})
}

func TestTopologyResolverBackgroundTimeoutAndRetry(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var calls atomic.Int32
		timeoutErrors := make(chan error, 1)
		r := resolverFunc(func(ctx context.Context, _ string) (resolver.ResolveResult, *time.Time, error) {
			call := calls.Add(1)
			next := time.Now().Add(time.Minute)
			if call == 1 {
				return resolver.ResolveResult{Generation: 1, Addrs: []*resolver.Addr{resolver.NewAddr("tcp", "a:11211", 0)}}, &next, nil
			}
			if call == 2 {
				<-ctx.Done()
				timeoutErrors <- ctx.Err()
				next = time.Now().Add(time.Minute)
				return resolver.ResolveResult{}, &next, ctx.Err()
			}
			return resolver.ResolveResult{Generation: 2, Addrs: []*resolver.Addr{resolver.NewAddr("tcp", "b:11211", 0)}}, nil, nil
		})
		top, err := newTopology(t.Context(), "directory", r, time.Second, func(addr *resolver.Addr) *node { return newTestNode(t, addr, 4) }, nil)
		require.NoError(t, err)
		defer func() { require.NoError(t, top.close()) }()
		initial := topologyView(top)
		time.Sleep(time.Minute)
		synctest.Wait()
		require.Equal(t, int32(2), calls.Load())
		require.Empty(t, timeoutErrors)
		time.Sleep(time.Second)
		synctest.Wait()
		require.ErrorIs(t, <-timeoutErrors, context.DeadlineExceeded)
		require.Equal(t, initial, topologyView(top))
		time.Sleep(time.Minute)
		synctest.Wait()
		require.Equal(t, int32(3), calls.Load())
		view := topologyView(top)
		require.Len(t, view.addrs, 1, "a successful retry must publish the recovered topology")
		require.Equal(t, "b:11211", view.addrs[0].Address)
		require.Equal(t, uint64(2), view.generation)
	})
}

func TestTopologyResolveAcceptsCachedResultAfterTimeout(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		addr := resolver.NewAddr("tcp", "a:11211", 0)
		top := newTestTopology(t, []*resolver.Addr{addr})
		before := topologyView(top)
		top.resolver = resolverFunc(func(ctx context.Context, _ string) (resolver.ResolveResult, *time.Time, error) {
			<-ctx.Done()
			next := time.Now().Add(time.Minute)
			return resolver.ResolveResult{Generation: 1, Addrs: []*resolver.Addr{addr}}, &next, nil
		})
		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		defer cancel()
		next, err := top.resolve(ctx)
		require.NoError(t, err, "a resolver may recover a timeout with its complete cached result")
		require.NotNil(t, next)
		require.Equal(t, before, topologyView(top))
	})
}

func TestTopologyCloseRejectsLateResolveSuccess(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		top := newTestTopology(t, []*resolver.Addr{resolver.NewAddr("tcp", "a:11211", 0)})
		started := make(chan struct{})
		proceed := make(chan struct{})
		finish := sync.OnceFunc(func() { close(proceed) })
		defer finish()
		top.resolver = resolverFunc(func(context.Context, string) (resolver.ResolveResult, *time.Time, error) {
			close(started)
			<-proceed
			// Discovery can finish successfully after the topology has closed.
			return resolver.ResolveResult{Generation: 2, Addrs: []*resolver.Addr{resolver.NewAddr("tcp", "b:11211", 0)}}, nil, nil
		})
		finished := make(chan error, 1)
		go func() {
			_, err := top.resolve(t.Context())
			finished <- err
		}()
		<-started
		require.NoError(t, top.close())
		finish()
		err := <-finished
		require.ErrorIs(t, err, ErrClientClosed, "a late result must not publish after shutdown")
		_, closedErr := top.allNodes()
		require.ErrorIs(t, closedErr, ErrClientClosed)
		require.Equal(t, "a:11211", topologyView(top).addrs[0].Address, "a late result must not replace retained fields")
	})
}

func TestTopologyResolveGeneration(t *testing.T) {
	for _, test := range []struct {
		name       string
		generation uint64
	}{
		{name: "unversioned"},
		{name: "unchanged", generation: 1},
		{name: "changed", generation: 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			a := resolver.NewAddr("tcp", "a:11211", 0)
			b := resolver.NewAddr("tcp", "b:11211", 0)
			top := newTestTopology(t, []*resolver.Addr{a})
			before := topologyView(top)
			next := time.Now().Add(time.Hour)
			top.resolver = resolverFunc(func(context.Context, string) (resolver.ResolveResult, *time.Time, error) {
				return resolver.ResolveResult{Generation: test.generation, Addrs: []*resolver.Addr{b}}, &next, nil
			})
			gotNext, err := top.resolve(t.Context())
			require.NoError(t, err)
			require.NotNil(t, gotNext)
			require.Equal(t, next, *gotNext)
			if test.generation == 1 {
				require.Equal(t, before, topologyView(top), "the same generation preserves the published topology")
			} else {
				view := topologyView(top)
				require.Len(t, view.addrs, 1)
				require.Equal(t, "b:11211", view.addrs[0].Address)
				require.Equal(t, test.generation, view.generation)
				requirePoolClosed(t, before.nodes[a.AddrKey].pool)
			}
		})
	}
}

func TestTopologyOwnsResolverClose(t *testing.T) {
	for _, initialFailure := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "initial failure"}[initialFailure], func(t *testing.T) {
			r := &closingTopologyResolver{resolverFunc: func(context.Context, string) (resolver.ResolveResult, *time.Time, error) {
				if initialFailure {
					return resolver.ResolveResult{}, nil, errors.New("discovery failed")
				}
				return resolver.ResolveResult{Addrs: []*resolver.Addr{resolver.NewAddr("tcp", "a:11211", 0)}}, nil, nil
			}}
			top, err := newTopology(t.Context(), "directory", r, time.Second, func(addr *resolver.Addr) *node { return newTestNode(t, addr, 4) }, nil)
			if initialFailure {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.NoError(t, top.close())
				require.NoError(t, top.close())
			}
			require.EqualValues(t, 1, r.closes.Load())
		})
	}
}

func TestTopologyResolvePublishesSuccessfulResultAfterCancellation(t *testing.T) {
	top := newTestTopology(t, []*resolver.Addr{resolver.NewAddr("tcp", "a:11211", 0)})
	before := topologyView(top)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	top.resolver = resolverFunc(func(context.Context, string) (resolver.ResolveResult, *time.Time, error) {
		cancel()
		return resolver.ResolveResult{Generation: 2, Addrs: []*resolver.Addr{resolver.NewAddr("tcp", "b:11211", 0)}}, nil, nil
	})
	_, err := top.resolve(ctx)
	require.NoError(t, err, "a resolver's successful result remains publishable after cancellation")
	require.ErrorIs(t, ctx.Err(), context.Canceled)
	view := topologyView(top)
	require.Len(t, view.addrs, 1)
	require.Equal(t, "b:11211", view.addrs[0].Address)
	require.Equal(t, uint64(2), view.generation)
	requirePoolClosed(t, before.nodes[resolver.AddrKey{Network: "tcp", Address: "a:11211"}].pool)
}

type closingTopologyResolver struct {
	resolverFunc
	closes atomic.Int32
}

func (r *closingTopologyResolver) Close() error { r.closes.Add(1); return nil }

type resolverFunc func(context.Context, string) (resolver.ResolveResult, *time.Time, error)

func (resolverFunc) Close() error { return nil }

func (f resolverFunc) Resolve(ctx context.Context, target string) (resolver.ResolveResult, *time.Time, error) {
	return f(ctx, target)
}

type testPickerFunc func([]*resolver.Addr, []byte, []byte) (*resolver.Addr, error)

func (p testPickerFunc) Pick(addrs []*resolver.Addr, cmd, key []byte) (*resolver.Addr, error) {
	return p(addrs, cmd, key)
}

func newTestTopology(t *testing.T, addrs []*resolver.Addr, metrics ...*telemetry.Metrics) *topology {
	t.Helper()
	r := resolverFunc(func(context.Context, string) (resolver.ResolveResult, *time.Time, error) {
		return resolver.ResolveResult{Addrs: addrs, Generation: 1}, nil, nil
	})
	var m *telemetry.Metrics
	if len(metrics) != 0 {
		m = metrics[0]
	}
	top, err := newTopology(t.Context(), "directory", r, time.Second, func(addr *resolver.Addr) *node {
		return newTestNode(t, addr, 4)
	}, m)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, top.close()) })
	return top
}

type topologySnapshot struct {
	addrs      []*resolver.Addr
	nodes      map[resolver.AddrKey]*node
	generation uint64
}

// Match discovery's lock; published addresses remain immutable.
func topologyView(top *topology) topologySnapshot {
	top.mu.RLock()
	defer top.mu.RUnlock()
	return topologySnapshot{addrs: slices.Clone(top.cachedAddrs), nodes: maps.Clone(top.nodes), generation: top.generation}
}

func updateTopology(c *client, addrs []*resolver.Addr) error {
	return applyTopology(c.topology, addrs)
}

func applyTopology(top *topology, addrs []*resolver.Addr) error {
	return top.apply(context.Background(), resolver.ResolveResult{Addrs: addrs})
}

func TestTopologyEmitsDiscoveryMetrics(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		meter := &testutil.CaptureMeter{}
		initialAddrs := []*resolver.Addr{
			resolver.NewAddr("tcp", "a.example:11211", 0),
			resolver.NewAddr("tcp", "b.example:11211", 0),
		}
		calls := 0
		failure := errors.New("directory temporarily unavailable")
		r := resolverFunc(func(context.Context, string) (resolver.ResolveResult, *time.Time, error) {
			calls++
			// Fake time exposes duration without waiting on a discovery schedule.
			time.Sleep(100 * time.Millisecond)
			switch calls {
			case 1:
				return resolver.ResolveResult{Generation: 1, Addrs: initialAddrs}, nil, nil
			case 2:
				return resolver.ResolveResult{}, nil, failure
			default:
				return resolver.ResolveResult{Generation: 2, Addrs: []*resolver.Addr{
					resolver.NewAddr("tcp", "b.example:11211", 0),
					resolver.NewAddr("tcp", "c.example:11211", 0),
					resolver.NewAddr("tcp", "d.example:11211", 0),
				}}, nil, nil
			}
		})
		top, err := newTopology(t.Context(), "directory", r, time.Second, func(addr *resolver.Addr) *node { return newTestNode(t, addr, 4) }, telemetry.NewConfig(telemetry.WithMeterProvider(meter.Provider())).Metrics())
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, top.close()) })
		require.NotNil(t, top.metrics)
		require.Equal(t, []float64{1}, meter.Values("memcached.discovery.resolve.calls"))
		require.Empty(t, meter.Values("memcached.discovery.resolve.errors"))
		firstSuccess := float64(time.Now().Unix())
		require.Equal(t, []float64{firstSuccess}, meter.Values("memcached.discovery.resolve.last_success"))
		require.Equal(t, []float64{2}, meter.Values("memcached.topology.nodes"))
		require.Equal(t, []float64{1}, meter.Values("memcached.topology.generation"))

		_, err = top.resolve(t.Context())
		require.ErrorIs(t, err, failure)
		require.Equal(t, []float64{1, 1}, meter.Values("memcached.discovery.resolve.calls"))
		require.Equal(t, []float64{1}, meter.Values("memcached.discovery.resolve.errors"))
		require.Equal(t, []float64{firstSuccess}, meter.Values("memcached.discovery.resolve.last_success"),
			"failed discovery must preserve the last successful timestamp")
		require.Equal(t, []float64{2}, meter.Values("memcached.topology.nodes"),
			"failed discovery must preserve the current topology")

		time.Sleep(time.Second)
		_, err = top.resolve(t.Context())
		require.NoError(t, err)
		require.Equal(t, []float64{1, 1, 1}, meter.Values("memcached.discovery.resolve.calls"))
		require.Equal(t, []float64{1}, meter.Values("memcached.discovery.resolve.errors"))
		require.Equal(t, []float64{0.1, 0.1, 0.1}, meter.Values("memcached.discovery.resolve.duration"))
		successes := meter.Values("memcached.discovery.resolve.last_success")
		require.Len(t, successes, 2)
		require.Equal(t, firstSuccess, successes[0])
		require.Greater(t, successes[1], firstSuccess)
		require.Equal(t, []float64{2, 3}, meter.Values("memcached.topology.nodes"))
		require.Equal(t, []float64{1, 2}, meter.Values("memcached.topology.generation"))

		beforeClose := meter.Measurements()
		require.NoError(t, top.close())
		require.NoError(t, top.close())
		require.Equal(t, beforeClose, meter.Measurements(), "close must not publish topology measurements")

		var clientID string
		for _, point := range meter.Measurements() {
			require.Equal(t, 1, point.Attributes.Len(), "discovery attributes must not expose the target or topology")
			id, ok := point.Attributes.Value("memcached.client.id")
			require.True(t, ok)
			require.NotEmpty(t, id.AsString())
			if clientID == "" {
				clientID = id.AsString()
			}
			require.Equal(t, clientID, id.AsString())
		}
	})
}

func TestTopologyUnchangedGenerationOnlyUpdatesDiscoveryMetrics(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		meter := &testutil.CaptureMeter{}
		top := newTestTopology(t, []*resolver.Addr{
			resolver.NewAddr("tcp", "a.example:11211", 0),
			resolver.NewAddr("tcp", "b.example:11211", 0),
		}, telemetry.NewConfig(telemetry.WithMeterProvider(meter.Provider())).Metrics())
		top.resolver = resolverFunc(func(context.Context, string) (resolver.ResolveResult, *time.Time, error) {
			time.Sleep(100 * time.Millisecond)
			// New address objects and reordered membership describe the same topology.
			return resolver.ResolveResult{Generation: 1, Addrs: []*resolver.Addr{
				resolver.NewAddr("tcp", "b.example:11211", 0),
				resolver.NewAddr("tcp", "a.example:11211", 0),
			}}, nil, nil
		})
		firstSuccess := meter.Values("memcached.discovery.resolve.last_success")
		require.Len(t, firstSuccess, 1)

		time.Sleep(time.Second)
		_, err := top.resolve(t.Context())
		require.NoError(t, err)
		require.Equal(t, []float64{1, 1}, meter.Values("memcached.discovery.resolve.calls"))
		require.Empty(t, meter.Values("memcached.discovery.resolve.errors"))
		successes := meter.Values("memcached.discovery.resolve.last_success")
		require.Len(t, successes, 2)
		require.Equal(t, firstSuccess[0], successes[0])
		require.Greater(t, successes[1], successes[0], "unchanged topology still counts as successful discovery")
		require.Equal(t, []float64{1}, meter.Values("memcached.topology.generation"),
			"an unchanged source generation must not emit another topology measurement")
		require.Equal(t, []float64{2}, meter.Values("memcached.topology.nodes"))
	})
}

func TestTopologySourceGenerationChangeUpdatesGauge(t *testing.T) {
	meter := &testutil.CaptureMeter{}
	addrs := []*resolver.Addr{resolver.NewAddr("tcp", "a.example:11211", 0)}
	top := newTestTopology(t, addrs, telemetry.NewConfig(telemetry.WithMeterProvider(meter.Provider())).Metrics())
	require.NoError(t, top.apply(t.Context(), resolver.ResolveResult{Generation: 2, Addrs: addrs}))
	require.Equal(t, []float64{1, 2}, meter.Values("memcached.topology.generation"),
		"source generation changes must be observable without replacing nodes")
	require.Equal(t, []float64{1, 1}, meter.Values("memcached.topology.nodes"))
}
