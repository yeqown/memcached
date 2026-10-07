package memcached

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"
)

func TestConnPoolCloseClosesIdleConnections(t *testing.T) {
	raw := newPoolLifecycleConn()
	pool := newConnPool(1, 1, 0, 0, func(context.Context) (memcachedConn, error) {
		return raw, nil
	})
	t.Cleanup(func() { require.NoError(t, pool.close()) })
	cn, err := pool.get(t.Context())
	require.NoError(t, err)
	require.NoError(t, pool.put(cn))
	require.NoError(t, pool.close())
	require.EqualValues(t, 1, raw.closes.Load())
	require.Zero(t, pool.stats().TotalConns)
}

func TestConnPoolGetInterruptedByClose(t *testing.T) {
	for _, stage := range []string{"waiting for capacity", "dialing"} {
		t.Run(stage, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				raw := newPoolLifecycleConn()
				started, proceed := make(chan struct{}), make(chan struct{})
				pool := newConnPool(1, 1, 0, 0, func(context.Context) (memcachedConn, error) {
					if stage == "dialing" {
						close(started)
						<-proceed
					}
					return raw, nil
				})
				t.Cleanup(func() { require.NoError(t, pool.close()) })
				var borrowed memcachedConn
				if stage == "waiting for capacity" {
					var err error
					borrowed, err = pool.get(t.Context())
					require.NoError(t, err)
				}
				result := make(chan poolGetResult, 1)
				go func() {
					cn, err := pool.get(t.Context())
					result <- poolGetResult{cn: cn, err: err}
				}()
				if stage == "dialing" {
					<-started
				}
				synctest.Wait()
				require.Empty(t, result)
				require.NoError(t, pool.close())
				if stage == "dialing" {
					close(proceed)
				}
				got := awaitPoolGet(t, result)
				require.Nil(t, got.cn)
				require.ErrorContains(t, got.err, "connection pool is closed")
				if borrowed != nil {
					require.NoError(t, pool.put(borrowed))
				}
				require.EqualValues(t, 1, raw.closes.Load())
				require.Zero(t, pool.stats().TotalConns)
			})
		})
	}
}

func TestConnPoolDialRetainsCapacityUntilCompletion(t *testing.T) {
	dialErr := errors.New("dial failed")
	for _, test := range []struct {
		name      string
		firstErr  error
		wantDials int32
	}{
		{name: "success", wantDials: 1},
		{name: "failure", firstErr: dialErr, wantDials: 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				var calls atomic.Int32
				started, proceed := make(chan struct{}), make(chan struct{})
				finish := sync.OnceFunc(func() { close(proceed) })
				defer finish()
				pool := newConnPool(1, 1, 0, 0, func(context.Context) (memcachedConn, error) {
					if calls.Add(1) == 1 {
						close(started)
						<-proceed
						if test.firstErr != nil {
							return nil, test.firstErr
						}
					}
					return newPoolLifecycleConn(), nil
				})
				t.Cleanup(func() { require.NoError(t, pool.close()) })
				first, second := make(chan poolGetResult, 1), make(chan poolGetResult, 1)
				go func() {
					cn, err := pool.get(t.Context())
					first <- poolGetResult{cn: cn, err: err}
				}()
				<-started
				go func() {
					cn, err := pool.get(t.Context())
					second <- poolGetResult{cn: cn, err: err}
				}()
				synctest.Wait()
				require.EqualValues(t, 1, calls.Load(), "a pending dial retains the only connection slot")
				require.Empty(t, second)
				finish()
				gotFirst := awaitPoolGet(t, first)
				if test.firstErr != nil {
					require.ErrorIs(t, gotFirst.err, test.firstErr)
					require.Nil(t, gotFirst.cn)
				} else {
					require.NoError(t, gotFirst.err)
					require.NoError(t, pool.put(gotFirst.cn))
				}
				gotSecond := awaitPoolGet(t, second)
				require.NoError(t, gotSecond.err)
				require.NotNil(t, gotSecond.cn)
				require.NoError(t, pool.put(gotSecond.cn))
				require.Equal(t, test.wantDials, calls.Load())
				require.Equal(t, 1, pool.stats().TotalConns)
			})
		})
	}
}

func TestConnPoolPutReturnsCloseError(t *testing.T) {
	for _, reason := range []string{"pool closed", "idle limit"} {
		t.Run(reason, func(t *testing.T) {
			pool := newConnPool(1, 2, 0, 0, func(context.Context) (memcachedConn, error) {
				return newPoolLifecycleConn(), nil
			})
			t.Cleanup(func() { require.NoError(t, pool.close()) })
			idle, err := pool.get(t.Context())
			require.NoError(t, err)
			borrowed, err := pool.get(t.Context())
			require.NoError(t, err)
			require.NoError(t, pool.put(idle))
			if reason == "pool closed" {
				require.NoError(t, pool.close())
			}
			raw := borrowed.(*poolLifecycleConn)
			closeErr := errors.New("connection close failed")
			raw.closeErr = closeErr
			require.ErrorIs(t, pool.put(borrowed), closeErr)
			require.EqualValues(t, 1, raw.closes.Load())
		})
	}
}

type poolLifecycleConn struct {
	*mockConn
	closes   atomic.Int32
	onClose  func()
	closeErr error
}

func newPoolLifecycleConn() *poolLifecycleConn {
	return &poolLifecycleConn{mockConn: newMockConn()}
}

func (c *poolLifecycleConn) Close() error {
	c.closes.Add(1)
	if c.onClose != nil {
		c.onClose()
	}
	return c.closeErr
}

type poolGetResult struct {
	cn  memcachedConn
	err error
}

func requirePoolClosed(t *testing.T, pool *connPool) {
	t.Helper()
	cn, err := pool.get(t.Context())
	require.ErrorContains(t, err, "connection pool is closed")
	require.Nil(t, cn)
}

func awaitPoolGet(t *testing.T, result <-chan poolGetResult) poolGetResult {
	t.Helper()
	select {
	case got := <-result:
		return got
	case <-time.After(time.Second):
		t.Fatal("pool get did not finish")
		return poolGetResult{}
	}
}

func TestConnPoolReusesReturnedConnections(t *testing.T) {
	var calls int
	pool := newConnPool(3, 3, 0, 0, func(context.Context) (memcachedConn, error) {
		calls++
		return newPoolLifecycleConn(), nil
	})
	t.Cleanup(func() { require.NoError(t, pool.close()) })
	conns := make([]memcachedConn, 3)
	for i := range conns {
		cn, err := pool.get(t.Context())
		require.NoError(t, err)
		require.Same(t, pool, cn.getConnPool())
		conns[i] = cn
	}
	require.Equal(t, 3, pool.stats().TotalConns)
	require.Zero(t, pool.stats().IdleConns)
	for _, cn := range conns {
		require.NoError(t, pool.put(cn))
	}
	stats := pool.stats()
	require.Equal(t, 3, stats.TotalConns)
	require.Equal(t, 3, stats.IdleConns)
	require.Equal(t, 3, stats.MaxConns)
	require.Equal(t, 3, stats.MaxIdle)
	for _, want := range conns {
		cn, err := pool.get(t.Context())
		require.NoError(t, err)
		require.Same(t, want, cn)
	}
	require.Equal(t, 3, calls, "reuse must not dial new connections")
	require.NoError(t, pool.close())
	for _, cn := range conns {
		require.NoError(t, pool.put(cn))
	}
	require.Zero(t, pool.stats().TotalConns)
}

func TestConnPoolPutEnforcesMaxIdle(t *testing.T) {
	for _, test := range []struct {
		name      string
		maxIdle   int
		wantIdle  int
		wantClose int64
	}{
		{name: "one idle", maxIdle: 1, wantIdle: 1, wantClose: 2},
		{name: "two idle", maxIdle: 2, wantIdle: 2, wantClose: 1},
		{name: "no idle limit", maxIdle: 0, wantIdle: 3, wantClose: 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			pool := newConnPool(test.maxIdle, 3, 0, 0, func(context.Context) (memcachedConn, error) {
				return newPoolLifecycleConn(), nil
			})
			t.Cleanup(func() { require.NoError(t, pool.close()) })
			conns := make([]memcachedConn, 3)
			for i := range conns {
				cn, err := pool.get(t.Context())
				require.NoError(t, err)
				conns[i] = cn
			}
			for _, cn := range conns {
				require.NoError(t, pool.put(cn))
			}
			stats := pool.stats()
			require.Equal(t, test.wantIdle, stats.TotalConns)
			require.Equal(t, test.wantIdle, stats.IdleConns)
			require.Equal(t, test.wantClose, stats.maxIdleClosed)
			for _, cn := range conns[test.wantIdle:] {
				require.EqualValues(t, 1, cn.(*poolLifecycleConn).closes.Load())
			}
			for _, want := range conns[:test.wantIdle] {
				cn, err := pool.get(t.Context())
				require.NoError(t, err)
				require.Same(t, want, cn)
			}
			require.NoError(t, pool.close())
			for _, cn := range conns[:test.wantIdle] {
				require.NoError(t, pool.put(cn))
			}
			require.Zero(t, pool.stats().TotalConns)
		})
	}
}

func TestConnPoolGetWaitsForReturnedConnection(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		pool := newConnPool(1, 1, 0, 0, createConn)
		t.Cleanup(func() { require.NoError(t, pool.close()) })
		cn, err := pool.get(t.Context())
		require.NoError(t, err)
		result := make(chan poolGetResult, 1)
		go func() {
			cn, err := pool.get(t.Context())
			result <- poolGetResult{cn, err}
		}()
		synctest.Wait()
		select {
		case <-result:
			t.Fatal("get must wait while the maximum connections are in use")
		default:
		}
		require.NoError(t, pool.put(cn))
		got := awaitPoolGet(t, result)
		require.NoError(t, got.err)
		require.Same(t, cn, got.cn)
		require.Equal(t, 1, pool.stats().TotalConns)
		require.Zero(t, pool.stats().IdleConns)
		require.NoError(t, pool.close())
		require.NoError(t, pool.put(got.cn))
	})
}

func TestConnPoolGetWaitRespectsContext(t *testing.T) {
	for _, deadline := range []bool{false, true} {
		name := "canceled"
		if deadline {
			name = "deadline exceeded"
		}
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				pool := newConnPool(1, 1, 0, 0, createConn)
				t.Cleanup(func() { require.NoError(t, pool.close()) })
				cn, err := pool.get(t.Context())
				require.NoError(t, err)
				var ctx context.Context
				var cancel context.CancelFunc
				if deadline {
					ctx, cancel = context.WithTimeout(t.Context(), time.Second)
				} else {
					ctx, cancel = context.WithCancel(t.Context())
				}
				defer cancel()
				result := make(chan poolGetResult, 1)
				go func() {
					cn, err := pool.get(ctx)
					result <- poolGetResult{cn, err}
				}()
				synctest.Wait()
				select {
				case <-result:
					t.Fatal("get must wait for a returned connection or context cancellation")
				default:
				}
				wantErr := context.Canceled
				if deadline {
					wantErr = context.DeadlineExceeded
					time.Sleep(time.Second)
				} else {
					cancel()
				}
				got := awaitPoolGet(t, result)
				require.ErrorIs(t, got.err, wantErr)
				require.Nil(t, got.cn)
				require.Equal(t, 1, pool.stats().TotalConns)
				require.Zero(t, pool.stats().IdleConns)
				require.NoError(t, pool.put(cn))
				reused, err := pool.get(t.Context())
				require.NoError(t, err)
				require.Same(t, cn, reused)
				require.NoError(t, pool.close())
				require.NoError(t, pool.put(reused))
			})
		})
	}
}

func TestConnPoolDialFailureDoesNotConsumeCapacity(t *testing.T) {
	failure := errors.New("dial failed")
	calls := 0
	pool := newConnPool(1, 1, 0, 0, func(context.Context) (memcachedConn, error) {
		calls++
		if calls == 1 {
			return nil, failure
		}
		return newPoolLifecycleConn(), nil
	})
	t.Cleanup(func() { require.NoError(t, pool.close()) })
	cn, err := pool.get(t.Context())
	require.ErrorIs(t, err, failure)
	require.Nil(t, cn)
	require.Zero(t, pool.stats().TotalConns)
	cn, err = pool.get(t.Context())
	require.NoError(t, err)
	require.NotNil(t, cn)
	require.Equal(t, 1, pool.stats().TotalConns)
	require.NoError(t, pool.close())
	require.NoError(t, pool.put(cn))
}

func TestConnPoolGetDialTimeout(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		pool := newConnPool(1, 1, 0, 0, func(ctx context.Context) (memcachedConn, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		})
		t.Cleanup(func() { require.NoError(t, pool.close()) })
		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		defer cancel()
		cn, err := pool.get(ctx)
		require.ErrorIs(t, err, context.DeadlineExceeded)
		require.Nil(t, cn)
		require.Zero(t, pool.stats().TotalConns, "failed dials must not consume connection capacity")
	})
}

func TestConnPoolCleanerExpiresOnlyIdleConnections(t *testing.T) {
	for _, test := range []struct {
		name                   string
		lifetime, idleTimeout  time.Duration
		wantIdle, wantLifetime int64
	}{
		{name: "idle timeout", idleTimeout: time.Second, wantIdle: 1},
		{name: "maximum lifetime", lifetime: time.Second, wantLifetime: 1},
		{name: "both limits", lifetime: time.Second, idleTimeout: time.Second, wantIdle: 1, wantLifetime: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				pool := newConnPool(4, 4, test.lifetime, test.idleTimeout, func(context.Context) (memcachedConn, error) {
					return newPoolLifecycleConn(), nil
				})
				t.Cleanup(func() { require.NoError(t, pool.close()) })
				retained, err := pool.get(t.Context())
				require.NoError(t, err)
				borrowed, err := pool.get(t.Context())
				require.NoError(t, err)
				borrowed.(*poolLifecycleConn).createdAt = time.Now().Add(-time.Hour)
				borrowed.(*poolLifecycleConn).returnedAt = time.Now().Add(-time.Hour)
				var expired []memcachedConn
				if test.idleTimeout > 0 {
					cn, err := pool.get(t.Context())
					require.NoError(t, err)
					cn.(*poolLifecycleConn).returnedAt = time.Now().Add(-time.Hour)
					expired = append(expired, cn)
				}
				if test.lifetime > 0 {
					cn, err := pool.get(t.Context())
					require.NoError(t, err)
					cn.(*poolLifecycleConn).createdAt = time.Now().Add(-time.Hour)
					expired = append(expired, cn)
				}
				for _, cn := range expired {
					require.NoError(t, pool.put(cn))
				}
				require.NoError(t, pool.put(retained))
				time.Sleep(time.Second)
				synctest.Wait()
				stats := pool.stats()
				require.Equal(t, 2, stats.TotalConns)
				require.Equal(t, 1, stats.IdleConns)
				require.Equal(t, test.wantIdle, stats.maxIdleTimeClosed)
				require.Equal(t, test.wantLifetime, stats.maxLifeTimeClosed)
				require.Zero(t, stats.maxIdleClosed)
				for _, cn := range expired {
					require.EqualValues(t, 1, cn.(*poolLifecycleConn).closes.Load())
				}
				require.Zero(t, borrowed.(*poolLifecycleConn).closes.Load())
				reused, err := pool.get(t.Context())
				require.NoError(t, err)
				require.Same(t, retained, reused)
				require.NoError(t, pool.close())
				synctest.Wait()
				require.NoError(t, pool.put(reused))
				require.NoError(t, pool.put(borrowed))
				require.Zero(t, pool.stats().TotalConns)
			})
		})
	}
}

func TestConnPoolGetAfterClose(t *testing.T) {
	pool := newConnPool(1, 1, 0, 0, func(context.Context) (memcachedConn, error) {
		t.Fatal("closed pool must not create a connection")
		return nil, nil
	})
	require.NoError(t, pool.close())
	require.NoError(t, pool.close())
	cn, err := pool.get(t.Context())
	require.Error(t, err)
	require.Nil(t, cn)
}

func TestConnPoolPutAfterClose(t *testing.T) {
	raw := newPoolLifecycleConn()
	pool := newConnPool(1, 1, 0, 0, func(context.Context) (memcachedConn, error) { return raw, nil })
	cn, err := pool.get(t.Context())
	require.NoError(t, err)
	require.NoError(t, pool.close())
	require.Zero(t, raw.closes.Load(), "borrowed connections close when returned")
	require.NoError(t, pool.put(cn))
	require.EqualValues(t, 1, raw.closes.Load())
	require.Zero(t, pool.stats().TotalConns)
	require.Zero(t, pool.stats().IdleConns)
}
