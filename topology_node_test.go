package memcached

import (
	"context"
	"io"
	"net"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/yeqown/memcached/resolver"
)

func TestNodeRemovalAndReaddUsesNewPool(t *testing.T) {
	a, b := resolver.NewAddr("tcp", "a:11211", 0), resolver.NewAddr("tcp", "b:11211", 0)
	c := topologyClient(t, []*resolver.Addr{a})
	old := clientView(c).nodes[a.AddrKey]
	raw := &nodeLifecycleConn{newPoolLifecycleConn()}
	old.pool.createConn = func(context.Context) (memcachedConn, error) { return raw, nil }
	_, releaseOld, err := old.getConn(t.Context())
	require.NoError(t, err)
	defer releaseOld()
	require.NoError(t, updateTopology(c, []*resolver.Addr{b}))
	require.Equal(t, nodeDraining, old.status())
	require.NoError(t, updateTopology(c, []*resolver.Addr{a, b}))
	current := clientView(c).nodes[a.AddrKey]
	require.NotSame(t, old, current)
	require.NotSame(t, old.pool, current.pool)
	require.Zero(t, raw.closes.Load())
	releaseOld()
	require.Equal(t, nodeClosed, old.status())
	require.EqualValues(t, 1, raw.closes.Load())
	require.Equal(t, nodeAvailable, current.status())
}

func TestNodeRemovalRejectsWaitingBorrow(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		a := resolver.NewAddr("tcp", "a:11211", 0)
		c := topologyClient(t, []*resolver.Addr{a}, WithMaxConns(1))
		n := clientView(c).nodes[a.AddrKey]
		raw := &nodeLifecycleConn{newPoolLifecycleConn()}
		n.pool.createConn = func(context.Context) (memcachedConn, error) { return raw, nil }
		_, releaseOwner, err := n.getConn(t.Context())
		require.NoError(t, err)
		defer releaseOwner()
		result := make(chan poolGetResult, 1)
		go func() {
			cn, releaseFn, err := n.getConn(t.Context())
			if releaseFn != nil {
				releaseFn()
			}
			result <- poolGetResult{cn, err}
		}()
		synctest.Wait()
		require.Empty(t, result)
		require.NoError(t, updateTopology(c, []*resolver.Addr{resolver.NewAddr("tcp", "b:11211", 0)}))
		got := <-result
		require.Error(t, got.err)
		require.Nil(t, got.cn)
		require.Equal(t, nodeDraining, n.status())
		require.Zero(t, raw.closes.Load())
		releaseOwner()
		require.Equal(t, nodeClosed, n.status())
		require.EqualValues(t, 1, raw.closes.Load())
	})
}

func TestNodeRemovalRejectsDialingBorrow(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		a := resolver.NewAddr("tcp", "a:11211", 0)
		c := topologyClient(t, []*resolver.Addr{a})
		n := clientView(c).nodes[a.AddrKey]
		started, proceed := make(chan struct{}), make(chan struct{})
		raw := &nodeLifecycleConn{newPoolLifecycleConn()}
		n.pool.createConn = func(context.Context) (memcachedConn, error) {
			close(started)
			<-proceed
			return raw, nil
		}
		result := make(chan poolGetResult, 1)
		go func() {
			cn, releaseFn, err := n.getConn(t.Context())
			if releaseFn != nil {
				releaseFn()
			}
			result <- poolGetResult{cn, err}
		}()
		<-started
		require.NoError(t, updateTopology(c, []*resolver.Addr{resolver.NewAddr("tcp", "b:11211", 0)}))
		require.Equal(t, nodeDraining, n.status(), "dial reservation keeps draining incomplete")
		close(proceed)
		got := <-result
		require.Error(t, got.err)
		require.Nil(t, got.cn)
		require.EqualValues(t, 1, raw.closes.Load())
		require.Equal(t, nodeClosed, n.status())
	})
}

type nodeLifecycleConn struct {
	*poolLifecycleConn
}

func (c *nodeLifecycleConn) release() error { return c.pool.put(c) }

func TestNodeConfirmsAvailabilityAfterPoolBorrow(t *testing.T) {
	a := resolver.NewAddr("tcp", "a:11211", 0)
	c := topologyClient(t, []*resolver.Addr{a})
	n := clientView(c).nodes[a.AddrKey]
	raw := &nodeLifecycleConn{newPoolLifecycleConn()}
	n.pool.createConn = func(context.Context) (memcachedConn, error) {
		// Model close's interval between marking draining and stopping the pool.
		// The pool can still return a connection, but node must reject the borrow.
		n.state.Store(uint32(nodeDraining))
		return raw, nil
	}
	cn, releaseFn, err := n.getConn(t.Context())
	if releaseFn != nil {
		releaseFn()
	}
	require.ErrorIs(t, err, ErrInstanceAbnormal)
	require.Nil(t, cn)
	require.Nil(t, releaseFn)
	require.Equal(t, 1, n.pool.stats().IdleConns, "rejected connection must be returned")
	// Complete the pool shutdown after manually marking the node as draining.
	require.NoError(t, n.pool.close())
	require.EqualValues(t, 1, raw.closes.Load())
	require.Equal(t, nodeClosed, n.status())
}

func TestRemovedNodeRejectsSelectedButUnborrowedRequest(t *testing.T) {
	a := resolver.NewAddr("tcp", "a:11211", 0)
	c := topologyClient(t, []*resolver.Addr{a})
	n, err := c.topology.pickNode(c.options.picker, nil, nil)
	require.NoError(t, err)
	n.pool.createConn = func(context.Context) (memcachedConn, error) {
		return &nodeLifecycleConn{newPoolLifecycleConn()}, nil
	}
	require.NoError(t, updateTopology(c, []*resolver.Addr{resolver.NewAddr("tcp", "b:11211", 0)}))
	cn, releaseFn, err := n.getConn(t.Context())
	if cn != nil {
		releaseFn()
	}
	require.ErrorIs(t, err, ErrInstanceAbnormal)
	require.Nil(t, cn)
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
