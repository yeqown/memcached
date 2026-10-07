package memcached

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"net"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/yeqown/memcached/picker"
	"github.com/yeqown/memcached/resolver"
)

func TestClientCloseConcurrent(t *testing.T) {
	c := topologyClient(t, []*resolver.Addr{resolver.NewAddr("tcp", "a:11211", 0)})
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() { require.NoError(t, c.Close()) })
	}
	wg.Wait()
	_, err := c.Version(context.Background())
	require.ErrorIs(t, err, ErrClientClosed)
}

func TestClientCloseConcurrentClosesAllPools(t *testing.T) {
	c := topologyClient(t, []*resolver.Addr{
		resolver.NewAddr("tcp", "a:11211", 0), resolver.NewAddr("tcp", "b:11211", 0),
	})
	initial := clientView(c)
	results := make(chan error, 20)
	for range cap(results) {
		go func() { results <- c.Close() }()
	}
	for range cap(results) {
		select {
		case err := <-results:
			require.NoError(t, err)
		case <-time.After(time.Second):
			t.Fatal("concurrent Close did not finish")
		}
	}
	for _, inst := range initial.nodes {
		requirePoolClosed(t, inst.pool)
	}
	_, closedErr := c.topology.allNodes()
	require.ErrorIs(t, closedErr, ErrClientClosed)
}

func topologyClient(t *testing.T, addrs []*resolver.Addr, opts ...ClientOption) *client {
	t.Helper()
	initial := WithResolver(resolverFunc(func(context.Context, string) (resolver.ResolveResult, *time.Time, error) {
		return resolver.ResolveResult{Addrs: addrs, Generation: 1}, nil, nil
	}))
	cc, err := New("directory", append([]ClientOption{initial}, opts...)...)
	require.NoError(t, err)
	c := cc.(*client)
	t.Cleanup(func() { require.NoError(t, c.Close()) })
	return c
}

func updateTopology(c *client, addrs []*resolver.Addr) error {
	return c.topology.apply(context.Background(), resolver.ResolveResult{Addrs: addrs})
}

// Capture routing under the same lock used by discovery; addresses stay immutable.
type clientTopologyView struct {
	addrs      []*resolver.Addr
	nodes      map[resolver.AddrKey]*node
	generation uint64
}

func clientView(c *client) clientTopologyView {
	c.topology.mu.RLock()
	defer c.topology.mu.RUnlock()
	return clientTopologyView{addrs: slices.Clone(c.topology.cachedAddrs), nodes: maps.Clone(c.topology.nodes), generation: c.topology.generation}
}

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
			c := topologyClient(t, []*resolver.Addr{a, b})
			old := clientView(c)
			require.NoError(t, updateTopology(c, test.next))
			current := clientView(c)
			require.Len(t, current.nodes, len(test.next))
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

func TestClientTopologyUpdatesRoutingInputsForSameNode(t *testing.T) {
	addr := resolver.NewAddr("tcp", "cache.example:11211", 1)
	addr.Add("zone", "one")
	c := topologyClient(t, []*resolver.Addr{addr})
	initial := clientView(c)

	updated := resolver.NewAddr("tcp", "cache.example:11211", 2)
	updated.Add("zone", "two")
	require.True(t, addr.AddrKey.Equal(updated.AddrKey), "priority and metadata do not change node identity")

	require.NoError(t, updateTopology(c, []*resolver.Addr{updated}))
	current := clientView(c)
	require.Equal(t, 2, current.addrs[0].Priority)
	require.Equal(t, "two", current.addrs[0].GetMetadata("zone"))
	id := addr.AddrKey
	require.Same(t, initial.nodes[id], current.nodes[id], "the same node keeps its connection pool")
}

func TestClientRemovalDoesNotWaitForRequestsOnOtherNodes(t *testing.T) {
	gate := make(chan struct{})
	finish := sync.OnceFunc(func() { close(gate) })
	defer finish()
	a, b := startDiscoveryNode(t, "a"), startDiscoveryNode(t, "b", gate)
	selectB := testPickerFunc(func(addrs []*resolver.Addr, _, _ []byte) (*resolver.Addr, error) {
		for _, addr := range addrs {
			if addr.Address == b.address {
				return addr, nil
			}
		}
		return nil, ErrInvalidAddress
	})
	c := topologyClient(t, []*resolver.Addr{resolver.NewAddr("tcp", a.address, 0), resolver.NewAddr("tcp", b.address, 0)}, WithPicker(selectB))
	targets, err := c.topology.allNodes()
	require.NoError(t, err)
	var selected *node
	for _, inst := range targets {
		if inst.addr.Address == a.address {
			selected = inst
		}
	}
	require.NotNil(t, selected)
	cn, releaseFn, err := selected.getConn(context.Background())
	require.NoError(t, err)
	pool := cn.getConnPool()
	releaseFn()
	done := make(chan error, 1)
	go func() { _, err := c.Version(context.Background()); done <- err }()
	select {
	case <-b.entered:
	case <-time.After(time.Second):
		t.Fatal("request did not reach retained node")
	}
	require.NoError(t, updateTopology(c, []*resolver.Addr{resolver.NewAddr("tcp", b.address, 0)}))
	requirePoolClosed(t, pool)
	finish()
	require.NoError(t, <-done)
}

func TestClientCloseAggregatesNodeErrors(t *testing.T) {
	firstErr, secondErr := errors.New("first node close failed"), errors.New("second node close failed")
	cc, err := New("a:11211,b:11211", WithMaxConns(1), WithMaxIdleConns(1))
	require.NoError(t, err)
	// The close errors are expected and checked below; cleanup must still run if setup fails.
	t.Cleanup(func() { _ = cc.Close() })
	c := cc.(*client)
	view := clientView(c)
	raws := make([]*poolLifecycleConn, 0, 2)
	for _, test := range []struct {
		address string
		err     error
	}{
		{address: "a:11211", err: firstErr},
		{address: "b:11211", err: secondErr},
	} {
		raw := newPoolLifecycleConn()
		raw.closeErr = test.err
		raws = append(raws, raw)
		n := view.nodes[resolver.AddrKey{Network: "tcp", Address: test.address}]
		n.pool.createConn = func(context.Context) (memcachedConn, error) { return raw, nil }
		cn, getErr := n.pool.get(t.Context())
		require.NoError(t, getErr)
		require.NoError(t, n.pool.put(cn))
	}
	closeErr := cc.Close()
	require.ErrorIs(t, closeErr, firstErr)
	require.ErrorIs(t, closeErr, secondErr)
	require.NoError(t, cc.Close(), "repeated close must not retry node cleanup")
	for _, raw := range raws {
		require.EqualValues(t, 1, raw.closes.Load())
	}
}

func TestClientRejectsInvalidInitialTopology(t *testing.T) {
	for _, test := range []struct {
		name  string
		addrs []*resolver.Addr
	}{
		{name: "empty"},
		{name: "nil node", addrs: []*resolver.Addr{nil}},
		{name: "conflicting identity", addrs: []*resolver.Addr{resolver.NewAddr("tcp", "a:11211", 1), resolver.NewAddr("tcp", "a:11211", 2)}},
	} {
		t.Run(test.name, func(t *testing.T) {
			cc, err := New("directory", WithResolver(resolverFunc(func(context.Context, string) (resolver.ResolveResult, *time.Time, error) {
				return resolver.ResolveResult{Addrs: test.addrs}, nil, nil
			})))
			if cc != nil {
				require.NoError(t, cc.Close())
			}
			require.ErrorIs(t, err, ErrInvalidAddress)
			require.Nil(t, cc)
		})
	}
}

func TestClientTopologyOwnsRoutingInputs(t *testing.T) {
	a := resolver.NewAddr("tcp", "cache.example:11211", 1)
	a.Add("zone", "one")
	c := topologyClient(t, []*resolver.Addr{resolver.NewAddr("tcp", "other:11211", 0)})
	require.NoError(t, updateTopology(c, []*resolver.Addr{a}))
	a.Address, a.Priority = "changed:11211", 9
	a.Add("zone", "changed")
	view := clientView(c)
	require.Equal(t, "cache.example:11211", view.addrs[0].Address)
	require.Equal(t, 1, view.addrs[0].Priority)
	require.Equal(t, "one", view.addrs[0].GetMetadata("zone"))
}

func TestTopologyInvalidRefreshPreservesNodes(t *testing.T) {
	a := resolver.NewAddr("tcp", "a:11211", 0)
	c := topologyClient(t, []*resolver.Addr{a})
	before := clientView(c)
	for _, addrs := range [][]*resolver.Addr{nil, {nil}, {a, a.Clone()}} {
		require.Error(t, updateTopology(c, addrs))
		require.Equal(t, before, clientView(c))
	}
}

func TestTopologyCloseDrainsRemovedNode(t *testing.T) {
	c := topologyClient(t, []*resolver.Addr{resolver.NewAddr("tcp", "a:11211", 0)})
	n, err := c.topology.pickNode(c.options.picker, nil, nil)
	require.NoError(t, err)
	raw := &nodeLifecycleConn{newPoolLifecycleConn()}
	n.pool.createConn = func(context.Context) (memcachedConn, error) { return raw, nil }
	_, releaseFn, err := n.getConn(t.Context())
	require.NoError(t, err)
	defer releaseFn()
	require.NoError(t, updateTopology(c, []*resolver.Addr{resolver.NewAddr("tcp", "b:11211", 0)}))
	require.NoError(t, c.Close())
	require.Equal(t, nodeDraining, n.status())
	require.Zero(t, raw.closes.Load(), "shutdown preserves the borrowed connection")
	releaseFn()
	requirePoolClosed(t, n.pool)
	require.EqualValues(t, 1, raw.closes.Load())
	require.Equal(t, nodeClosed, n.status())
}

func startClientDiscoveryEndpoint(t *testing.T, respond func(net.Conn) error) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	var handlers sync.WaitGroup
	var connections sync.Map
	acceptDone := make(chan struct{})
	handlers.Add(1)
	go func() {
		defer handlers.Done()
		defer close(acceptDone)
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			connections.Store(conn, struct{}{})
			handlers.Add(1)
			go func() {
				defer handlers.Done()
				defer connections.Delete(conn)
				defer func() { _ = conn.Close() }()
				reader := bufio.NewReader(conn)
				for {
					command, err := reader.ReadString('\n')
					if err != nil {
						return
					}
					if command != "config get cluster\r\n" {
						t.Errorf("unexpected discovery command %q", command)
						return
					}
					if err := respond(conn); err != nil {
						t.Errorf("discovery response: %v", err)
						return
					}
				}
			}()
		}
	}()
	t.Cleanup(func() {
		_ = listener.Close()
		<-acceptDone
		connections.Range(func(key, _ any) bool { _ = key.(net.Conn).Close(); return true })
		handlers.Wait()
	})
	return listener.Addr().String()
}

func clientConfigResponse(version string, nodes ...*discoveryNode) string {
	entries := make([]string, len(nodes))
	for i, node := range nodes {
		host, port, err := net.SplitHostPort(node.address)
		if err != nil {
			panic(err)
		}
		entries[i] = "|" + host + "|" + port
	}
	payload := version + "\n" + strings.Join(entries, " ") + "\n"
	return fmt.Sprintf("CONFIG cluster 0 %d\r\n%s\r\nEND\r\n", len(payload), payload)
}

func TestClientAutoDiscoveryAppliesTopologyChanges(t *testing.T) {
	a := startDiscoveryNode(t, "node-a")
	b := startDiscoveryNode(t, "node-b")
	responses := make(chan string, 4)
	requests := make(chan struct{}, 8)
	responses <- clientConfigResponse("1", a)
	target := startClientDiscoveryEndpoint(t, func(conn net.Conn) error {
		requests <- struct{}{}
		select {
		case response := <-responses:
			_, err := io.WriteString(conn, response)
			return err
		case <-t.Context().Done():
			return nil
		}
	})
	cc, err := New(target, WithResolver(resolver.NewAutoDiscovery(10*time.Millisecond)))
	require.NoError(t, err)
	c := cc.(*client)
	defer func() { require.NoError(t, c.Close()) }()
	waitRequest := func() {
		t.Helper()
		select {
		case <-requests:
		case <-time.After(2 * time.Second):
			t.Fatal("discovery request did not arrive")
		}
	}
	waitRequest()
	initial := clientView(c)
	require.Len(t, initial.addrs, 1, "initial discovery must publish its data node")
	version, err := c.Version(context.Background())
	require.NoError(t, err)
	require.Equal(t, "node-a", version)

	responses <- "SERVER_ERROR discovery unavailable\r\n"
	waitRequest()
	waitRequest() // The next retry proves the failed result has been consumed.
	require.Equal(t, initial, clientView(c))
	responses <- clientConfigResponse("2", b, a)
	require.Eventually(t, func() bool { return clientView(c).generation == 2 }, 2*time.Second, time.Millisecond)
	expanded := clientView(c)
	require.Len(t, expanded.addrs, 2)
	require.Same(t, initial.nodes[resolver.AddrKey{Network: "tcp", Address: a.address}], expanded.nodes[resolver.AddrKey{Network: "tcp", Address: a.address}])
	require.NoError(t, c.FlushAll(context.Background()))
	require.EqualValues(t, 1, a.flushes.Load())
	require.EqualValues(t, 1, b.flushes.Load())

	waitRequest()
	responses <- clientConfigResponse("3", b)
	require.Eventually(t, func() bool { return clientView(c).generation == 3 }, 2*time.Second, time.Millisecond)
	require.Len(t, clientView(c).addrs, 1)
	version, err = c.Version(context.Background())
	require.NoError(t, err)
	require.Equal(t, "node-b", version)
}

func TestClientCloseCancelsAutoDiscoveryRead(t *testing.T) {
	node := startDiscoveryNode(t, "node-a")
	entered := make(chan struct{})
	disconnected := make(chan struct{})
	var calls atomic.Int32
	target := startClientDiscoveryEndpoint(t, func(conn net.Conn) error {
		if calls.Add(1) == 1 {
			_, err := io.WriteString(conn, clientConfigResponse("1", node))
			return err
		}
		close(entered)
		_, err := io.Copy(io.Discard, conn)
		close(disconnected)
		return err
	})
	c, err := New(target, WithResolver(resolver.NewAutoDiscovery(10*time.Millisecond)), WithResolveTimeout(time.Minute))
	require.NoError(t, err)
	defer func() { require.NoError(t, c.Close()) }()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("background discovery did not start")
	}
	closed := make(chan error, 1)
	go func() { closed <- c.Close() }()
	select {
	case err := <-closed:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("Close did not cancel discovery")
	}
	select {
	case <-disconnected:
	case <-time.After(2 * time.Second):
		t.Fatal("Close left a discovery connection open")
	}
}

func TestClientResolverScheduleAndError(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		calls := 0
		r := resolverFunc(func(ctx context.Context, target string) (resolver.ResolveResult, *time.Time, error) {
			require.Equal(t, "http://directory", target)
			_, ok := ctx.Deadline()
			require.True(t, ok)
			calls++
			next := time.Now().Add(time.Hour)
			switch calls {
			case 1:
				return resolver.ResolveResult{Addrs: []*resolver.Addr{resolver.NewAddr("tcp", "a:11211", 0)}}, &next, nil
			case 2:
				return resolver.ResolveResult{}, &next, errors.New("directory unavailable")
			default:
				return resolver.ResolveResult{Addrs: []*resolver.Addr{resolver.NewAddr("tcp", "a:11211", 0)}}, nil, nil
			}
		})
		cc, err := New("http://directory", WithResolver(r))
		require.NoError(t, err)
		defer func() { require.NoError(t, cc.Close()) }()
		synctest.Wait()
		require.Equal(t, 1, calls)
		time.Sleep(time.Hour - time.Second)
		synctest.Wait()
		require.Equal(t, 1, calls, "resolution must wait for the supplied schedule")
		time.Sleep(time.Second)
		synctest.Wait()
		require.Equal(t, 2, calls)
		time.Sleep(time.Hour)
		synctest.Wait()
		require.Equal(t, 3, calls, "an error with a retry schedule must continue discovery")
		time.Sleep(24 * time.Hour)
		synctest.Wait()
		require.Equal(t, 3, calls, "a nil schedule stops discovery")
	})
}

func TestClientResolverNilErrorScheduleStopsRefresh(t *testing.T) {
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
		cc, err := New("directory", WithResolver(r))
		require.NoError(t, err)
		defer func() { require.NoError(t, cc.Close()) }()
		time.Sleep(time.Minute)
		synctest.Wait()
		require.Equal(t, 2, calls)
		time.Sleep(time.Hour)
		synctest.Wait()
		require.Equal(t, 2, calls, "a nil schedule also stops discovery on error")
	})
}

func TestClientResolverTimeoutAndClose(t *testing.T) {
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
				cc, err := New("directory", WithResolver(r), WithResolveTimeout(time.Second))
				if initial {
					require.ErrorIs(t, err, context.DeadlineExceeded)
					require.ErrorIs(t, <-resolveErrors, context.DeadlineExceeded)
					return
				}
				require.NoError(t, err)
				time.Sleep(time.Minute)
				synctest.Wait()
				require.NoError(t, cc.Close())
				require.Empty(t, resolveErrors, "closing a custom resolver does not cancel its attempt context")
				time.Sleep(time.Second)
				synctest.Wait()
				require.ErrorIs(t, <-resolveErrors, context.DeadlineExceeded)
				require.NoError(t, cc.Close())
				_, closedErr := cc.(*client).topology.allNodes()
				require.ErrorIs(t, closedErr, ErrClientClosed)
			})
		})
	}
}

func TestClientResolverCustomInitialTimeout(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := resolverFunc(func(ctx context.Context, _ string) (resolver.ResolveResult, *time.Time, error) {
			select {
			case <-time.After(6 * time.Second):
				return resolver.ResolveResult{Addrs: []*resolver.Addr{resolver.NewAddr("tcp", "a:11211", 0)}}, nil, nil
			case <-ctx.Done():
				return resolver.ResolveResult{}, nil, ctx.Err()
			}
		})
		c, err := New("directory", WithResolver(r), WithResolveTimeout(10*time.Second))
		require.NoError(t, err, "the configured timeout must also apply during initialization")
		require.NoError(t, c.Close())
	})
}

func TestClientResolverOwnsLifecycle(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var calls atomic.Int32
		r := resolverFunc(func(context.Context, string) (resolver.ResolveResult, *time.Time, error) {
			call := calls.Add(1)
			if call == 3 {
				return resolver.ResolveResult{Addrs: []*resolver.Addr{resolver.NewAddr("tcp", "a:11211", 0)}}, nil, nil
			}
			next := time.Now().Add(time.Hour)
			if call == 2 {
				next = time.Now().Add(30 * time.Minute)
			}
			return resolver.ResolveResult{Addrs: []*resolver.Addr{resolver.NewAddr("tcp", "a:11211", 0)}}, &next, nil
		})
		initialCtx, cancel := context.WithCancel(t.Context())
		defer cancel()
		cc, err := NewWithContext(initialCtx, "directory", WithResolver(r))
		require.NoError(t, err)
		defer func() { require.NoError(t, cc.Close()) }()
		cancel()
		time.Sleep(time.Hour)
		synctest.Wait()
		require.Equal(t, 2, int(calls.Load()), "canceling the constructor context must not stop background discovery")
		time.Sleep(29 * time.Minute)
		synctest.Wait()
		require.Equal(t, 2, int(calls.Load()))
		time.Sleep(time.Minute)
		synctest.Wait()
		require.Equal(t, 3, int(calls.Load()), "unchanged topology must still use the new schedule")
	})
}

func TestClientResolverCopiesReturnedSchedule(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		a := resolver.NewAddr("tcp", "a:11211", 0)
		c := topologyClient(t, []*resolver.Addr{a})
		var calls atomic.Int32
		schedule := time.Now().Add(time.Hour)
		c.topology.resolver = resolverFunc(func(context.Context, string) (resolver.ResolveResult, *time.Time, error) {
			call := calls.Add(1)
			result := resolver.ResolveResult{Generation: 1, Addrs: []*resolver.Addr{a}}
			if call == 1 {
				return result, &schedule, nil
			}
			return result, nil, nil
		})
		next, err := c.topology.resolve(t.Context())
		require.NoError(t, err)
		require.NotNil(t, next)
		// Mutate before starting the loop so this ownership check has no data race.
		schedule = time.Now().Add(time.Minute)
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		go c.topology.resolverLoop(ctx, next)
		time.Sleep(2 * time.Minute)
		synctest.Wait()
		require.Equal(t, 1, int(calls.Load()), "resolver-owned timestamp changes must not alter the captured schedule")
		time.Sleep(58 * time.Minute)
		synctest.Wait()
		require.Equal(t, 2, int(calls.Load()))
	})
}

func TestClientResolverBackgroundTimeoutAndRetry(t *testing.T) {
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
		cc, err := New("directory", WithResolver(r), WithResolveTimeout(time.Second))
		require.NoError(t, err)
		c := cc.(*client)
		defer func() { require.NoError(t, c.Close()) }()
		initial := clientView(c)
		time.Sleep(time.Minute)
		synctest.Wait()
		require.Equal(t, int32(2), calls.Load())
		require.Empty(t, timeoutErrors)
		time.Sleep(time.Second)
		synctest.Wait()
		require.ErrorIs(t, <-timeoutErrors, context.DeadlineExceeded)
		require.Equal(t, initial, clientView(c))
		time.Sleep(time.Minute)
		synctest.Wait()
		require.Equal(t, int32(3), calls.Load())
		view := clientView(c)
		require.Len(t, view.addrs, 1, "a successful retry must publish the recovered topology")
		require.Equal(t, "b:11211", view.addrs[0].Address)
		require.Equal(t, uint64(2), view.generation)
	})
}

func TestTopologyResolveAcceptsCachedResultAfterTimeout(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		addr := resolver.NewAddr("tcp", "a:11211", 0)
		c := topologyClient(t, []*resolver.Addr{addr})
		before := clientView(c)
		c.topology.resolver = resolverFunc(func(ctx context.Context, _ string) (resolver.ResolveResult, *time.Time, error) {
			<-ctx.Done()
			next := time.Now().Add(time.Minute)
			return resolver.ResolveResult{Generation: 1, Addrs: []*resolver.Addr{addr}}, &next, nil
		})
		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		defer cancel()
		next, err := c.topology.resolve(ctx)
		require.NoError(t, err, "a resolver may recover a timeout with its complete cached result")
		require.NotNil(t, next)
		require.Equal(t, before, clientView(c))
	})
}

func TestClientCloseRejectsLateResolveSuccess(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c := topologyClient(t, []*resolver.Addr{resolver.NewAddr("tcp", "a:11211", 0)})
		started := make(chan struct{})
		proceed := make(chan struct{})
		finish := sync.OnceFunc(func() { close(proceed) })
		defer finish()
		c.topology.resolver = resolverFunc(func(context.Context, string) (resolver.ResolveResult, *time.Time, error) {
			close(started)
			<-proceed
			// Discovery can finish successfully after the topology has closed.
			return resolver.ResolveResult{Generation: 2, Addrs: []*resolver.Addr{resolver.NewAddr("tcp", "b:11211", 0)}}, nil, nil
		})
		finished := make(chan error, 1)
		go func() {
			_, err := c.topology.resolve(t.Context())
			finished <- err
		}()
		<-started
		require.NoError(t, c.Close())
		finish()
		err := <-finished
		require.ErrorIs(t, err, ErrClientClosed, "a late result must not publish after shutdown")
		_, closedErr := c.topology.allNodes()
		require.ErrorIs(t, closedErr, ErrClientClosed)
		require.Equal(t, "a:11211", clientView(c).addrs[0].Address, "a late result must not replace retained fields")
	})
}

func TestClientStaticResolverRemainsUsableWithoutRefresh(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		cc, err := New("a:11211")
		require.NoError(t, err)
		c := cc.(*client)
		defer func() { require.NoError(t, c.Close()) }()
		time.Sleep(time.Hour)
		synctest.Wait()
		require.Len(t, clientView(c).addrs, 1, "static resolution must publish its node")
		inst, err := c.topology.pickNode(c.options.picker, nil, nil)
		require.NoError(t, err, "finishing static resolution must not close the client")
		require.Equal(t, "a:11211", inst.addr.Address)
		require.NoError(t, c.Close(), "shutdown must not wait for an unstarted resolver loop")
		_, err = c.topology.pickNode(c.options.picker, nil, nil)
		require.ErrorIs(t, err, ErrClientClosed)
	})
}

func TestNewWithContextPublishesInitialTopology(t *testing.T) {
	for _, generation := range []uint64{0, 7} {
		t.Run(fmt.Sprint(generation), func(t *testing.T) {
			addr := resolver.NewAddr("tcp", "cache.example:11211", 0)
			r := resolverFunc(func(context.Context, string) (resolver.ResolveResult, *time.Time, error) {
				return resolver.ResolveResult{Generation: generation, Addrs: []*resolver.Addr{addr}}, nil, nil
			})
			cc, err := NewWithContext(t.Context(), "directory", WithResolver(r))
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, cc.Close()) })
			c := cc.(*client)
			view := clientView(c)
			require.Len(t, view.addrs, 1)
			require.Equal(t, addr.AddrKey, view.addrs[0].AddrKey)
			require.Equal(t, generation, view.generation)
			inst, err := c.topology.pickNode(c.options.picker, nil, []byte("key"))
			require.NoError(t, err)
			require.Equal(t, addr.AddrKey, inst.addr.AddrKey)
		})
	}
}

func TestClientResolveGeneration(t *testing.T) {
	for _, generation := range []uint64{1, 2} {
		t.Run(fmt.Sprint(generation), func(t *testing.T) {
			a := resolver.NewAddr("tcp", "a:11211", 0)
			b := resolver.NewAddr("tcp", "b:11211", 0)
			c := topologyClient(t, []*resolver.Addr{a})
			before := clientView(c)
			next := time.Now().Add(time.Hour)
			c.topology.resolver = resolverFunc(func(context.Context, string) (resolver.ResolveResult, *time.Time, error) {
				return resolver.ResolveResult{Generation: generation, Addrs: []*resolver.Addr{b}}, &next, nil
			})
			gotNext, err := c.topology.resolve(t.Context())
			require.NoError(t, err)
			require.Equal(t, next, *gotNext)
			if generation == 1 {
				require.Equal(t, before, clientView(c), "the same generation preserves the published topology")
			} else {
				view := clientView(c)
				require.Equal(t, "b:11211", view.addrs[0].Address)
				require.Equal(t, uint64(2), view.generation)
			}
		})
	}
}

type resolverFunc func(context.Context, string) (resolver.ResolveResult, *time.Time, error)

func (resolverFunc) Close() error { return nil }

func (f resolverFunc) Resolve(ctx context.Context, target string) (resolver.ResolveResult, *time.Time, error) {
	return f(ctx, target)
}

type discoveryNode struct {
	address string
	flushes atomic.Int32
	gate    <-chan struct{}
	entered chan struct{}
}

func startDiscoveryNode(t *testing.T, version string, gate ...<-chan struct{}) *discoveryNode {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	n := &discoveryNode{address: l.Addr().String(), entered: make(chan struct{}, 100)}
	if len(gate) != 0 {
		n.gate = gate[0]
	}
	var valuesMu sync.Mutex
	values := make(map[string][]byte)
	var connections sync.Map
	var wg sync.WaitGroup
	acceptDone := make(chan struct{})
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer close(acceptDone)
		for {
			cn, acceptErr := l.Accept()
			if acceptErr != nil {
				return
			}
			connections.Store(cn, struct{}{})
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer connections.Delete(cn)
				defer func() { _ = cn.Close() }()
				r := bufio.NewReader(cn)
				for {
					line, readErr := r.ReadString('\n')
					if readErr != nil {
						return
					}
					switch line {
					case "version\r\n":
						select {
						case n.entered <- struct{}{}:
						default:
						}
						if n.gate != nil {
							<-n.gate
						}
						_, _ = fmt.Fprintf(cn, "VERSION %s\r\n", version)
					case "flush_all\r\n":
						n.flushes.Add(1)
						_, _ = cn.Write([]byte("OK\r\n"))
					default:
						fields := strings.Fields(line)
						if len(fields) == 0 {
							return
						}
						switch fields[0] {
						case "set":
							if len(fields) != 5 {
								return
							}
							size, err := strconv.Atoi(fields[4])
							if err != nil {
								return
							}
							body := make([]byte, size+2)
							if _, err := io.ReadFull(r, body); err != nil {
								return
							}
							valuesMu.Lock()
							values[fields[1]] = body[:size]
							valuesMu.Unlock()
							_, _ = cn.Write([]byte("STORED\r\n"))
						case "get", "gets", "gat", "gats":
							keys := fields[1:]
							if fields[0] == "gat" || fields[0] == "gats" {
								keys = fields[2:]
							}
							for _, key := range keys {
								valuesMu.Lock()
								value, ok := values[key]
								valuesMu.Unlock()
								if !ok {
									continue
								}
								suffix := ""
								if fields[0] == "gets" || fields[0] == "gats" {
									suffix = " 42"
								}
								_, _ = fmt.Fprintf(cn, "VALUE %s 0 %d%s\r\n%s\r\n", key, len(value), suffix, value)
							}
							_, _ = cn.Write([]byte("END\r\n"))
						default:
							return
						}
					}
				}
			}()
		}
	}()
	t.Cleanup(func() {
		_ = l.Close()
		<-acceptDone
		connections.Range(func(key, _ any) bool { _ = key.(net.Conn).Close(); return true })
		wg.Wait()
	})
	return n
}

func TestClientDiscoveryConcurrentRequestsBroadcastAndClose(t *testing.T) {
	a, b := startDiscoveryNode(t, "a"), startDiscoveryNode(t, "b")
	var calls atomic.Int32
	refreshed := make(chan struct{})
	refreshGate := make(chan struct{})
	allowRefresh := sync.OnceFunc(func() { close(refreshGate) })
	defer allowRefresh()
	r := resolverFunc(func(ctx context.Context, _ string) (resolver.ResolveResult, *time.Time, error) {
		if err := ctx.Err(); err != nil {
			return resolver.ResolveResult{}, nil, err
		}
		n := calls.Add(1)
		if n > 1 {
			select {
			case <-refreshGate:
			case <-ctx.Done():
				return resolver.ResolveResult{}, nil, ctx.Err()
			}
		}
		if n == 10 {
			close(refreshed)
		}
		addrs := []*resolver.Addr{resolver.NewAddr("tcp", a.address, 0)}
		if n%2 != 0 {
			addrs = append(addrs, resolver.NewAddr("tcp", b.address, 0))
		}
		next := time.Now().Add(time.Millisecond)
		return resolver.ResolveResult{Generation: uint64(n), Addrs: addrs}, &next, nil
	})
	cc, err := New("directory", WithResolver(r), WithPicker(picker.NewRendezvousHashPicker(42)), WithMaxConns(4), WithMaxIdleConns(2))
	require.NoError(t, err)
	c := cc.(*client)
	defer func() { require.NoError(t, c.Close()) }()
	initial := clientView(c)
	require.Len(t, initial.addrs, 2, "concurrent requests require the initial topology to be published")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var completed atomic.Int32
	var wg sync.WaitGroup
	errCh := make(chan error, 8)
	ready := make(chan struct{}, 8)
	for i := range 8 {
		wg.Go(func() {
			ready <- struct{}{}
			for {
				var err error
				if i%2 == 0 {
					var version string
					version, err = c.Version(ctx)
					if err == nil && version != "a" && version != "b" {
						err = fmt.Errorf("unexpected node response %q", version)
					}
				} else {
					err = c.FlushAll(ctx)
				}
				if errors.Is(err, ErrClientClosed) {
					return
				}
				if errors.Is(err, ErrInstanceAbnormal) || err != nil && strings.Contains(err.Error(), "connection pool is closed") {
					continue
				}
				if err != nil {
					errCh <- err
					return
				}
				completed.Add(1)
			}
		})
	}
	for range cap(ready) {
		select {
		case <-ready:
		case <-ctx.Done():
			t.Fatal("request workers did not start")
		}
	}
	allowRefresh()
	select {
	case <-refreshed:
	case <-ctx.Done():
		t.Fatal("discovery did not refresh while requests were running")
	}
	require.Eventually(t, func() bool { return clientView(c).generation >= 10 }, time.Second, time.Millisecond,
		"discovery must publish changed topologies while requests run")
	require.NoError(t, c.Close())
	wg.Wait()
	close(errCh)
	for err := range errCh {
		require.NoError(t, err)
	}
	require.Positive(t, completed.Load())
	_, closedErr := c.topology.allNodes()
	require.ErrorIs(t, closedErr, ErrClientClosed)
	for _, p := range initial.nodes {
		requirePoolClosed(t, p.pool)
	}
}

func TestClientUnversionedResolverRefreshesMembership(t *testing.T) {
	a, b := resolver.NewAddr("tcp", "a:11211", 0), resolver.NewAddr("tcp", "b:11211", 0)
	c := topologyClient(t, []*resolver.Addr{a})
	c.topology.resolver = resolverFunc(func(context.Context, string) (resolver.ResolveResult, *time.Time, error) {
		return resolver.ResolveResult{Addrs: []*resolver.Addr{b}}, nil, nil
	})
	_, err := c.topology.resolve(t.Context())
	require.NoError(t, err)
	require.Equal(t, "b:11211", clientView(c).addrs[0].Address)
}

type closingTopologyResolver struct {
	resolverFunc
	closes atomic.Int32
}

func (r *closingTopologyResolver) Close() error { r.closes.Add(1); return nil }

func TestTopologyOwnsResolverClose(t *testing.T) {
	for _, initialFailure := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "initial failure"}[initialFailure], func(t *testing.T) {
			r := &closingTopologyResolver{resolverFunc: func(context.Context, string) (resolver.ResolveResult, *time.Time, error) {
				if initialFailure {
					return resolver.ResolveResult{}, nil, errors.New("discovery failed")
				}
				return resolver.ResolveResult{Addrs: []*resolver.Addr{resolver.NewAddr("tcp", "a:11211", 0)}}, nil, nil
			}}
			cc, err := New("directory", WithResolver(r))
			if initialFailure {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.NoError(t, cc.Close())
				require.NoError(t, cc.Close())
			}
			require.EqualValues(t, 1, r.closes.Load())
		})
	}
}

func TestTopologyResolvePublishesSuccessfulResultAfterCancellation(t *testing.T) {
	c := topologyClient(t, []*resolver.Addr{resolver.NewAddr("tcp", "a:11211", 0)})
	before := clientView(c)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	c.topology.resolver = resolverFunc(func(context.Context, string) (resolver.ResolveResult, *time.Time, error) {
		cancel()
		return resolver.ResolveResult{Generation: 2, Addrs: []*resolver.Addr{resolver.NewAddr("tcp", "b:11211", 0)}}, nil, nil
	})
	_, err := c.topology.resolve(ctx)
	require.NoError(t, err, "a resolver's successful result remains publishable after cancellation")
	require.ErrorIs(t, ctx.Err(), context.Canceled)
	view := clientView(c)
	require.Len(t, view.addrs, 1)
	require.Equal(t, "b:11211", view.addrs[0].Address)
	require.Equal(t, uint64(2), view.generation)
	requirePoolClosed(t, before.nodes[resolver.AddrKey{Network: "tcp", Address: "a:11211"}].pool)
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
	c := topologyClient(t, []*resolver.Addr{resolver.NewAddr("tcp", "a:11211", 0)}, WithPicker(p))
	raw := newPoolLifecycleConn()
	clientView(c).nodes[resolver.AddrKey{Network: "tcp", Address: "a:11211"}].pool.createConn = func(context.Context) (memcachedConn, error) { return raw, nil }
	picked := make(chan struct{})
	var inst *node
	var pickErr error
	go func() {
		inst, pickErr = c.topology.pickNode(c.options.picker, nil, nil)
		close(picked)
	}()
	<-entered
	updated := make(chan struct{})
	go func() {
		require.NoError(t, updateTopology(c, []*resolver.Addr{resolver.NewAddr("tcp", "b:11211", 0)}))
		close(updated)
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
	<-updated
	cn, releaseFn, err := inst.getConn(context.Background())
	require.ErrorIs(t, err, ErrInstanceAbnormal, "selection does not reserve a connection before removal")
	require.Nil(t, cn)
	require.Nil(t, releaseFn)
	requirePoolClosed(t, inst.pool)
}
