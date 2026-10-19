package memcached

import (
	"context"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/yeqown/memcached/resolver"
)

func TestNodeBorrowAllocationBudget(t *testing.T) {
	n := newTestNode(t, resolver.NewAddr("tcp", "a:11211"), 1)
	ctx := context.Background()
	allocs := testing.AllocsPerRun(100, func() {
		var borrowed borrowedConn
		if err := n.getConn(ctx, &borrowed); err != nil {
			t.Fatal(err)
		}
		borrowed.release()
	})
	require.LessOrEqual(t, allocs, float64(1), "borrowing must only allocate the single-use release handle")
}

func TestNodeConcurrentReleaseWaitsForCleanup(t *testing.T) {
	n := newTestNode(t, resolver.NewAddr("tcp", "a:11211"), 1)
	raw := newTestConn()
	closing, proceed := make(chan struct{}), make(chan struct{})
	var unblock sync.Once
	defer unblock.Do(func() { close(proceed) })
	raw.onClose = func() {
		close(closing)
		<-proceed
	}
	n.pool.createConn = func(context.Context) (memcachedConn, error) { return raw, nil }
	_, releaseFn, err := getTestNodeConn(t.Context(), n)
	require.NoError(t, err)
	require.NoError(t, n.close())

	returned := make(chan struct{}, 2)
	go func() { releaseFn(); returned <- struct{}{} }()
	select {
	case <-closing:
	case <-time.After(time.Second):
		t.Fatal("release did not start connection cleanup")
	}
	started := make(chan struct{})
	go func() { close(started); releaseFn(); returned <- struct{}{} }()
	<-started
	var returnedEarly bool
	select {
	case <-returned:
		returnedEarly = true
	case <-time.After(20 * time.Millisecond):
	}
	unblock.Do(func() { close(proceed) })
	remaining := 2
	if returnedEarly {
		remaining--
	}
	for range remaining {
		select {
		case <-returned:
		case <-time.After(time.Second):
			t.Fatal("release did not finish after allowing cleanup")
		}
	}
	require.False(t, returnedEarly, "concurrent release calls must wait for cleanup to finish")
	releaseFn()
	require.EqualValues(t, 1, raw.closes.Load())
	require.Equal(t, nodeClosed, n.status())
	require.Zero(t, n.pool.stats().TotalConns)
}

func TestNodeReleaseDoesNotRepeatPanic(t *testing.T) {
	n := newTestNode(t, resolver.NewAddr("tcp", "a:11211"), 1)
	raw := newTestConn()
	const failure = "connection close panicked"
	raw.onClose = func() { panic(failure) }
	n.pool.createConn = func(context.Context) (memcachedConn, error) { return raw, nil }
	_, releaseFn, err := getTestNodeConn(t.Context(), n)
	require.NoError(t, err)
	require.NoError(t, n.close())
	// The deliberately panicking Close prevents the pool from releasing its slot.
	defer n.pool.releaseSlot()
	require.PanicsWithValue(t, failure, releaseFn)
	require.NotPanics(t, releaseFn, "a completed release must not repeat an earlier panic")
	require.EqualValues(t, 1, raw.closes.Load(), "a panicking cleanup must not be retried")
}

func TestNodeCloseRejectsWaitingBorrow(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		a := resolver.NewAddr("tcp", "a:11211")
		c := newTestClient(t, []*resolver.Addr{a}, WithMaxConns(1))
		n := clientView(c).nodes[*a]
		raw := newTestConn()
		n.pool.createConn = func(context.Context) (memcachedConn, error) { return raw, nil }
		_, releaseOwner, err := getTestNodeConn(t.Context(), n)
		require.NoError(t, err)
		defer releaseOwner()
		result := make(chan poolGetResult, 1)
		go func() {
			cn, releaseFn, err := getTestNodeConn(t.Context(), n)
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
		a := resolver.NewAddr("tcp", "a:11211")
		c := newTestClient(t, []*resolver.Addr{a})
		n := clientView(c).nodes[*a]
		started, proceed := make(chan struct{}), make(chan struct{})
		raw := newTestConn()
		n.pool.createConn = func(context.Context) (memcachedConn, error) {
			close(started)
			<-proceed
			return raw, nil
		}
		result := make(chan poolGetResult, 1)
		go func() {
			cn, releaseFn, err := getTestNodeConn(t.Context(), n)
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
	a := resolver.NewAddr("tcp", "a:11211")
	n := newTestNode(t, a, 1)
	raw := newTestConn()
	n.pool.createConn = func(context.Context) (memcachedConn, error) {
		// Model close's interval between marking draining and stopping the pool.
		// The pool can still return a connection, but node must reject the borrow.
		n.state.Store(uint32(nodeDraining))
		return raw, nil
	}
	cn, releaseFn, err := getTestNodeConn(t.Context(), n)
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

func getTestNodeConn(ctx context.Context, n *node) (memcachedConn, func(), error) {
	var borrowed borrowedConn
	if err := n.getConn(ctx, &borrowed); err != nil {
		return nil, nil, err
	}
	return borrowed.conn, borrowed.release, nil
}

func newTestNode(t *testing.T, addr *resolver.Addr, maxConns int) *node {
	t.Helper()
	n := &node{addr: addr.Clone()}
	n.pool = newConnPool(maxConns, maxConns, 0, 0, createTestConn)
	t.Cleanup(func() { require.NoError(t, n.close()) })
	return n
}

func requireNodeClosed(t *testing.T, n *node) {
	t.Helper()
	require.Eventually(t, func() bool { return n.status() == nodeClosed }, time.Second, time.Millisecond,
		"node must finish asynchronous connection cleanup")
	requirePoolClosed(t, n.pool)
}
