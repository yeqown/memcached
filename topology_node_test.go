package memcached

import (
	"context"
	"testing"
	"testing/synctest"

	"github.com/stretchr/testify/require"
	"github.com/yeqown/memcached/resolver"
)

func TestNodeCloseRejectsWaitingBorrow(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		a := resolver.NewAddr("tcp", "a:11211", 0)
		c := newTestClient(t, []*resolver.Addr{a}, WithMaxConns(1))
		n := clientView(c).nodes[a.AddrKey]
		raw := newTestConn()
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
		require.NoError(t, n.close())
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

func TestNodeCloseRejectsDialingBorrow(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		a := resolver.NewAddr("tcp", "a:11211", 0)
		c := newTestClient(t, []*resolver.Addr{a})
		n := clientView(c).nodes[a.AddrKey]
		started, proceed := make(chan struct{}), make(chan struct{})
		raw := newTestConn()
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
		require.NoError(t, n.close())
		require.Equal(t, nodeDraining, n.status(), "dial reservation keeps draining incomplete")
		close(proceed)
		got := <-result
		require.Error(t, got.err)
		require.Nil(t, got.cn)
		require.EqualValues(t, 1, raw.closes.Load())
		require.Equal(t, nodeClosed, n.status())
	})
}

func TestNodeConfirmsAvailabilityAfterPoolBorrow(t *testing.T) {
	a := resolver.NewAddr("tcp", "a:11211", 0)
	n := newTestNode(t, a, 1)
	raw := newTestConn()
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

func newTestNode(t *testing.T, addr *resolver.Addr, maxConns int) *node {
	t.Helper()
	n := &node{addr: addr.Clone()}
	n.pool = newConnPool(maxConns, maxConns, 0, 0, createTestConn)
	t.Cleanup(func() { require.NoError(t, n.close()) })
	return n
}
