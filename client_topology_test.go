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

	"github.com/yeqown/memcached/picker"
	"github.com/yeqown/memcached/resolver"

	"github.com/stretchr/testify/require"
)

type resolverFunc func(context.Context, string) (resolver.ResolveResult, *time.Time, error)

func (f resolverFunc) Resolve(ctx context.Context, target string) (resolver.ResolveResult, *time.Time, error) {
	return f(ctx, target)
}

func topologyClient(t *testing.T, addrs []*resolver.Addr, opts ...ClientOption) *client {
	t.Helper()
	options := newClientOptions()
	for _, opt := range opts {
		opt(options)
	}
	ctx, cancel := context.WithCancel(context.Background())
	c := &client{
		options: options, target: "directory", ctx: ctx, cancelFn: cancel,
		active: slices.Clone(addrs), instances: make(map[resolver.AddrKey]*topologyIns),
		generation: 1,
	}
	for _, addr := range addrs {
		c.instances[addr.AddrKey] = c.makeInstance(addr)
	}
	t.Cleanup(func() { require.NoError(t, c.Close()) })
	return c
}

// Capture routing under the same lock used by discovery; addresses stay immutable.
type clientTopologyView struct {
	addrs      []*resolver.Addr
	instances  map[resolver.AddrKey]*topologyIns
	generation uint64
}

func clientView(c *client) clientTopologyView {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return clientTopologyView{addrs: slices.Clone(c.active), instances: maps.Clone(c.instances), generation: c.generation}
}

func TestCompareTopologyMembership(t *testing.T) {
	a := resolver.NewAddr("tcp", "a:11211", 0)
	b := resolver.NewAddr("tcp", "b:11211", 0)
	c := resolver.NewAddr("tcp", "c:11211", 0)
	for _, test := range []struct {
		name      string
		old, next []*resolver.Addr
		equal     bool
		removed   []*resolver.Addr
	}{
		{name: "empty", equal: true},
		{name: "same members", old: []*resolver.Addr{a, b}, next: []*resolver.Addr{a, b}, equal: true},
		{name: "same identities in new objects", old: []*resolver.Addr{a}, next: []*resolver.Addr{resolver.NewAddr("tcp", "a:11211", 0)}, equal: true},
		{name: "initial nodes", next: []*resolver.Addr{a}},
		{name: "addition", old: []*resolver.Addr{a}, next: []*resolver.Addr{a, b}},
		{name: "removal", old: []*resolver.Addr{a, b}, next: []*resolver.Addr{b}, removed: []*resolver.Addr{a}},
		{name: "same size replacement", old: []*resolver.Addr{a, b}, next: []*resolver.Addr{b, c}, removed: []*resolver.Addr{a}},
		{name: "all removed", old: []*resolver.Addr{a, b}, removed: []*resolver.Addr{a, b}},
		{name: "network changes identity", old: []*resolver.Addr{a}, next: []*resolver.Addr{resolver.NewAddr("udp", "a:11211", 0)}, removed: []*resolver.Addr{a}},
	} {
		t.Run(test.name, func(t *testing.T) {
			equal, removed := compareTopology(test.old, test.next)
			require.Equal(t, test.equal, equal)
			require.Equal(t, test.removed, removed)
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

	c.updateAddrs([]*resolver.Addr{updated})
	current := clientView(c)
	require.Equal(t, 2, current.addrs[0].Priority)
	require.Equal(t, "two", current.addrs[0].GetMetadata("zone"))
	id := addr.AddrKey
	require.Same(t, initial.instances[id], current.instances[id], "the same node keeps its connection pool")
}

func TestClientInstanceSurvivesRemovalAndReadd(t *testing.T) {
	n := startDiscoveryNode(t, "old")
	a := resolver.NewAddr("tcp", n.address, 0)
	b := resolver.NewAddr("tcp", "other.example:11211", 0)
	c := topologyClient(t, []*resolver.Addr{a})
	old, err := c.pickInstance(nil, nil)
	require.NoError(t, err)
	releaseOld := sync.OnceFunc(old.release)
	defer releaseOld()
	c.updateAddrs([]*resolver.Addr{b})
	// Selection predates removal; even the first dial must still be allowed.
	cn, err := old.getConn(context.Background())
	require.NoError(t, err)
	pool := cn.getConnPool()
	require.NoError(t, cn.release())
	c.updateAddrs([]*resolver.Addr{a, b})
	current := clientView(c).instances[resolver.AddrKey{Network: "tcp", Address: n.address}]
	require.NotSame(t, old, current, "a rejoining node gets a new instance")
	require.NoError(t, current.acquire())
	cn, err = current.getConn(context.Background())
	require.NoError(t, err)
	require.NotSame(t, pool, cn.getConnPool(), "a rejoining node must not revive a dropped pool")
	require.NoError(t, cn.release())
	current.release()
	require.Positive(t, pool.stats().TotalConns, "the old request still retains its pool")
	releaseOld()
	requirePoolClosed(t, pool)
	require.Same(t, current, clientView(c).instances[resolver.AddrKey{Network: "tcp", Address: n.address}], "old retirement must not remove the rejoined instance")
}

func TestClientInstanceWaitersSurviveRemoval(t *testing.T) {
	for _, action := range []string{"return connection", "cancel request"} {
		t.Run(action, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				c := topologyClient(t, []*resolver.Addr{resolver.NewAddr("tcp", "a:11211", 0)}, WithMaxConns(1))
				owner, err := c.pickInstance(nil, nil)
				require.NoError(t, err)
				raw := newPoolLifecycleConn()
				owner.pool.createConn = func(context.Context) (memcachedConn, error) { return raw, nil }
				cn, err := owner.getConn(context.Background())
				require.NoError(t, err)
				waiter, err := c.pickInstance(nil, nil)
				require.NoError(t, err)
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				result := make(chan poolGetResult, 1)
				go func() { cn, err := waiter.getConn(ctx); result <- poolGetResult{cn, err} }()
				synctest.Wait()
				require.Empty(t, result, "the second request waits for pool capacity")
				c.updateAddrs([]*resolver.Addr{resolver.NewAddr("tcp", "b:11211", 0)})
				require.ErrorIs(t, owner.acquire(), ErrInstanceAbnormal, "dropped instances reject new request references")
				require.Zero(t, raw.closes.Load(), "removal must preserve an already borrowed connection")
				switch action {
				case "return connection":
					require.NoError(t, owner.pool.put(cn))
					owner.release()
					got := <-result
					require.NoError(t, got.err, "a retained waiter can borrow after node removal")
					require.Same(t, cn, got.cn)
					require.NoError(t, waiter.pool.put(got.cn))
				case "cancel request":
					cancel()
					require.ErrorIs(t, (<-result).err, context.Canceled)
					require.NoError(t, owner.pool.put(cn))
					owner.release()

				}
				waiter.release()
				requirePoolClosed(t, owner.pool)
			})
		})
	}
}

func TestClientInstanceDialSurvivesRemoval(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c := topologyClient(t, []*resolver.Addr{resolver.NewAddr("tcp", "a:11211", 0)})
		inst, err := c.pickInstance(nil, nil)
		require.NoError(t, err)
		started, proceed := make(chan struct{}), make(chan struct{})
		raw := newPoolLifecycleConn()
		inst.pool.createConn = func(ctx context.Context) (memcachedConn, error) {
			close(started)
			select {
			case <-proceed:
				return raw, nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		result := make(chan poolGetResult, 1)
		go func() { cn, err := inst.getConn(context.Background()); result <- poolGetResult{cn, err} }()
		<-started
		c.updateAddrs([]*resolver.Addr{resolver.NewAddr("tcp", "b:11211", 0)})
		synctest.Wait()
		require.Empty(t, result, "removal must not cancel a retained request's dial")
		close(proceed)
		got := <-result
		require.NoError(t, got.err)
		require.NoError(t, inst.pool.put(got.cn))
		inst.release()
		requirePoolClosed(t, inst.pool)
	})
}

type instanceLifecycleConn struct {
	*poolLifecycleConn
}

func (c *instanceLifecycleConn) release() error { return c.pool.put(c) }

func TestClientBroadcastRetainsTargetsBeforeBorrowing(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		a, b := resolver.NewAddr("tcp", "a:11211", 0), resolver.NewAddr("tcp", "b:11211", 0)
		c := topologyClient(t, []*resolver.Addr{a, b})
		initial := clientView(c)
		first := initial.instances[resolver.AddrKey{Network: "tcp", Address: "a:11211"}]
		second := initial.instances[resolver.AddrKey{Network: "tcp", Address: "b:11211"}]
		started, proceed := make(chan struct{}), make(chan struct{})
		rawA := &instanceLifecycleConn{newPoolLifecycleConn()}
		rawB := &instanceLifecycleConn{newPoolLifecycleConn()}
		first.pool.createConn = func(ctx context.Context) (memcachedConn, error) {
			close(started)
			select {
			case <-proceed:
				return rawA, nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		second.pool.createConn = func(context.Context) (memcachedConn, error) { return rawB, nil }
		called := make(chan *connPool, 2)
		done := make(chan error, 1)
		go func() {
			done <- c.broadcastRequest(context.Background(), func(_ context.Context, cn memcachedConn) error {
				called <- cn.getConnPool()
				return nil
			})
		}()
		<-started
		synctest.Wait()
		require.Same(t, second.pool, <-called)
		c.updateAddrs([]*resolver.Addr{b, resolver.NewAddr("tcp", "c:11211", 0)})
		close(proceed)
		require.NoError(t, <-done, "the removed target must remain available to its original broadcast")
		require.Same(t, first.pool, <-called)
		requirePoolClosed(t, first.pool)
		require.Zero(t, rawB.closes.Load(), "the retained node keeps its idle connection")
		require.Zero(t, clientView(c).instances[resolver.AddrKey{Network: "tcp", Address: "c:11211"}].pool.stats().TotalConns)
	})
}

func TestClientSelectionAcquiresInstanceBeforeUpdate(t *testing.T) {
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
	clientView(c).instances[resolver.AddrKey{Network: "tcp", Address: "a:11211"}].pool.createConn = func(context.Context) (memcachedConn, error) { return raw, nil }
	picked := make(chan struct{})
	var inst *topologyIns
	var pickErr error
	go func() {
		inst, pickErr = c.pickInstance(nil, nil)
		close(picked)
	}()
	<-entered
	updated := make(chan struct{})
	go func() {
		c.updateAddrs([]*resolver.Addr{resolver.NewAddr("tcp", "b:11211", 0)})
		close(updated)
	}()
	// Mutex waits are not durable in synctest; use a real bounded wait.
	select {
	case <-updated:
		t.Fatal("update split selection from reference acquisition")
	case <-time.After(20 * time.Millisecond):
	}
	finish()
	<-picked
	require.NoError(t, pickErr)
	<-updated
	cn, err := inst.getConn(context.Background())
	require.NoError(t, err, "the original selection must survive removal before dialing")
	require.NoError(t, inst.pool.put(cn))
	inst.release()
	requirePoolClosed(t, inst.pool)
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
	targets, err := c.acquireInstances()
	require.NoError(t, err)
	var selected *topologyIns
	for _, inst := range targets {
		if inst.addr.Address == a.address {
			selected = inst
		}
	}
	require.NotNil(t, selected)
	cn, err := selected.getConn(context.Background())
	require.NoError(t, err)
	pool := cn.getConnPool()
	require.NoError(t, cn.release())
	for _, inst := range targets {
		inst.release()
	}
	done := make(chan error, 1)
	go func() { _, err := c.Version(context.Background()); done <- err }()
	select {
	case <-b.entered:
	case <-time.After(time.Second):
		t.Fatal("request did not reach retained node")
	}
	c.updateAddrs([]*resolver.Addr{resolver.NewAddr("tcp", b.address, 0)})
	requirePoolClosed(t, pool)
	finish()
	require.NoError(t, <-done)
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

func TestClientRequestsKeepSelectedInstance(t *testing.T) {
	gate := make(chan struct{})
	release := sync.OnceFunc(func() { close(gate) })
	defer release()
	a, b := startDiscoveryNode(t, "a", gate), startDiscoveryNode(t, "b")
	c := topologyClient(t, []*resolver.Addr{resolver.NewAddr("tcp", a.address, 0)})
	done := make(chan string, 1)
	go func() {
		v, err := c.Version(context.Background())
		if err != nil {
			v = err.Error()
		}
		done <- v
	}()
	select {
	case <-a.entered:
	case <-time.After(time.Second):
		t.Fatal("request did not reach old node")
	}
	c.updateAddrs([]*resolver.Addr{resolver.NewAddr("tcp", b.address, 0)})
	require.Equal(t, b.address, clientView(c).addrs[0].Address, "the refreshed topology must route new requests to the new node")
	v, err := c.Version(context.Background())
	require.NoError(t, err)
	require.Equal(t, "b", v)
	release()
	require.Equal(t, "a", <-done)
	require.NoError(t, c.FlushAll(context.Background()))
	require.Equal(t, int32(0), a.flushes.Load())
	require.Equal(t, int32(1), b.flushes.Load())
}

type retrievalPicker struct{}

func (retrievalPicker) Pick(addrs []*resolver.Addr, _, key []byte) (*resolver.Addr, error) {
	if string(key) == "route-to-second" {
		return addrs[len(addrs)-1], nil
	}
	return addrs[0], nil
}

func TestClientRetrievalUsesKeyForRouting(t *testing.T) {
	a, b := startDiscoveryNode(t, "a"), startDiscoveryNode(t, "b")
	c := topologyClient(t, []*resolver.Addr{resolver.NewAddr("tcp", a.address, 0), resolver.NewAddr("tcp", b.address, 0)}, WithPicker(retrievalPicker{}))
	ctx := context.Background()
	require.NoError(t, c.Set(ctx, "route-to-second", []byte("value"), 0, 0))
	for _, cmd := range []string{"get", "gets", "gat", "gats"} {
		t.Run(cmd, func(t *testing.T) {
			var item *Item
			var items []*Item
			var err error
			switch cmd {
			case "get":
				item, err = c.Get(ctx, "route-to-second")
			case "gets":
				items, err = c.Gets(ctx, "route-to-second")
			case "gat":
				item, err = c.GetAndTouch(ctx, time.Minute, "route-to-second")
			case "gats":
				items, err = c.GetAndTouches(ctx, time.Minute, "route-to-second")
			}
			require.NoError(t, err)
			if item == nil {
				require.Len(t, items, 1)
				item = items[0]
			}
			require.Equal(t, []byte("value"), item.Value)
		})
	}
}

func TestClientTopologyBroadcastKeepsOriginalTargets(t *testing.T) {
	a, b, d := startDiscoveryNode(t, "a"), startDiscoveryNode(t, "b"), startDiscoveryNode(t, "d")
	c := topologyClient(t, []*resolver.Addr{resolver.NewAddr("tcp", a.address, 0), resolver.NewAddr("tcp", b.address, 0)})
	gate := make(chan struct{})
	release := sync.OnceFunc(func() { close(gate) })
	defer release()
	entered := make(chan string, 2)
	done := make(chan error, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	go func() {
		done <- c.broadcastRequest(ctx, func(ctx context.Context, cn memcachedConn) error {
			entered <- cn.(*conn).addr.String()
			select {
			case <-gate:
			case <-ctx.Done():
				return ctx.Err()
			}
			if _, err := cn.Write([]byte("flush_all\r\n")); err != nil {
				return err
			}
			_, err := cn.readLine('\n')
			return err
		})
	}()
	var targets []string
	for range 2 {
		select {
		case addr := <-entered:
			targets = append(targets, addr)
		case <-ctx.Done():
			t.Fatal("broadcast did not borrow both original connections")
		}
	}
	require.ElementsMatch(t, []string{a.address, b.address}, targets)
	initial := clientView(c)
	c.updateAddrs([]*resolver.Addr{resolver.NewAddr("tcp", b.address, 0), resolver.NewAddr("tcp", d.address, 0)})
	release()
	require.NoError(t, <-done)
	require.Equal(t, int32(1), a.flushes.Load())
	require.Equal(t, int32(1), b.flushes.Load())
	require.Zero(t, d.flushes.Load())
	requirePoolClosed(t, initial.instances[resolver.AddrKey{Network: "tcp", Address: a.address}].pool)
	require.NoError(t, c.FlushAll(ctx))
	require.Equal(t, int32(1), a.flushes.Load())
	require.Equal(t, int32(2), b.flushes.Load())
	require.Equal(t, int32(1), d.flushes.Load())
}

type testPickerFunc func([]*resolver.Addr, []byte, []byte) (*resolver.Addr, error)

func (p testPickerFunc) Pick(addrs []*resolver.Addr, cmd, key []byte) (*resolver.Addr, error) {
	return p(addrs, cmd, key)
}

func TestClientRejectsPickerOutsideActiveAddresses(t *testing.T) {
	for name, result := range map[string]*resolver.Addr{"nil": nil, "outside": resolver.NewAddr("tcp", "b:11211", 0)} {
		t.Run(name, func(t *testing.T) {
			picker := testPickerFunc(func([]*resolver.Addr, []byte, []byte) (*resolver.Addr, error) { return result, nil })
			c := topologyClient(t, []*resolver.Addr{resolver.NewAddr("tcp", "a:11211", 0)}, WithPicker(picker))
			_, err := c.Version(context.Background())
			require.ErrorIs(t, err, ErrInvalidAddress)
			for _, p := range clientView(c).instances {
				require.Zero(t, p.pool.stats().TotalConns)
			}
		})
	}
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
	cc, err := New("directory", WithResolver(r), WithPicker(picker.NewStableRendezvousHashPicker(42)), WithMaxConns(4), WithMaxIdleConns(2))
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
	require.Empty(t, clientView(c).addrs)
	require.Empty(t, c.instances)
	for _, p := range initial.instances {
		requirePoolClosed(t, p.pool)
	}
}

func TestClientRequestCancellationCancelsSASLHandshake(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer func() { _ = l.Close() }()
	started := make(chan struct{})
	serverDone := make(chan struct{})
	go func() {
		defer close(serverDone)
		cn, err := l.Accept()
		if err != nil {
			return
		}
		defer func() { _ = cn.Close() }()
		request := make([]byte, 24)
		if _, err := io.ReadFull(cn, request); err != nil {
			return
		}
		close(started)
		_, _ = io.Copy(io.Discard, cn) // Stall authentication until the request is canceled.
	}()
	c := topologyClient(t, []*resolver.Addr{resolver.NewAddr("tcp", l.Addr().String(), 0)}, WithSASL("user", "password"))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := c.Version(ctx); done <- err }()
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal("SASL handshake did not start")
	}
	cancel()
	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("request cancellation did not cancel authentication")
	}
	<-serverDone
}
