package memcached

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/pkg/errors"
	"github.com/stretchr/testify/assert"
)

var (
	_ memcachedConn = (*mockConn)(nil)
)

// mockConn is an implementation of the memcachedConn interface for testing purposes.
type mockConn struct {
	createdAt     time.Time
	returnedAt    time.Time
	readDeadline  time.Time
	writeDeadline time.Time
	pool          *connPool
}

func newMockConn() *mockConn {
	return &mockConn{
		createdAt:  time.Now(),
		returnedAt: time.Now(),
	}
}

func (m *mockConn) Read(_ []byte) (b int, err error) { return 0, nil }

func (m *mockConn) Write(_ []byte) (n int, err error) { return 0, nil }

func (m *mockConn) Close() error { return nil }

func (m *mockConn) readLine(_ byte) ([]byte, error) { return nil, nil }

func (m *mockConn) expired(since time.Time) (time.Duration, bool) {
	now := nowFunc()
	past := now.Sub(m.createdAt)
	if since.IsZero() {
		return past, false
	}

	return past, m.createdAt.Before(since)
}

func (m *mockConn) idle(since time.Time) (time.Duration, bool) {
	if since.IsZero() {
		return m.returnedAt.Sub(since), false
	}

	ok := m.returnedAt.Before(since)
	if ok {
		return 0, true
	}

	return m.returnedAt.Sub(since), false
}

func (m *mockConn) release() error {
	m.returnedAt = time.Now()
	return nil
}

func (m *mockConn) setConnPool(pool *connPool) { m.pool = pool }

func (m *mockConn) getConnPool() *connPool { return m.pool }

func (m *mockConn) setReadDeadline(d time.Time) error {
	if d.IsZero() {
		m.readDeadline = zeroTime
		return nil
	}

	m.readDeadline = d
	return nil
}

func (m *mockConn) setWriteDeadline(d time.Time) error {
	if d.IsZero() {
		m.writeDeadline = zeroTime
		return nil
	}

	m.writeDeadline = d
	return nil
}

func createConn(_ context.Context) (memcachedConn, error) {
	return newMockConn(), nil
}

func Test_connPool_new(t *testing.T) {
	tests := []struct {
		name           string
		maxIdle        int
		maxConn        int
		maxLifeTime    time.Duration
		maxIdleTimeout time.Duration
		wantErr        bool
	}{
		{
			name:           "basic initialization",
			maxIdle:        5,
			maxConn:        10,
			maxLifeTime:    time.Hour,
			maxIdleTimeout: 5 * time.Minute,
			wantErr:        false,
		},
		{
			name:           "zero max connections",
			maxIdle:        0,
			maxConn:        0,
			maxLifeTime:    time.Hour,
			maxIdleTimeout: time.Minute,
			wantErr:        false,
		},
		{
			name:           "maxIdle equals maxConn",
			maxIdle:        10,
			maxConn:        10,
			maxLifeTime:    time.Hour,
			maxIdleTimeout: time.Minute,
			wantErr:        false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pool := newConnPool(tt.maxIdle, tt.maxConn, tt.maxLifeTime, tt.maxIdleTimeout, createConn)

			// 验证基本属性
			assert.NotNil(t, pool)
			assert.Equal(t, tt.maxIdle, pool.maxIdle)
			assert.Equal(t, tt.maxConn, pool.maxConns)
			assert.Equal(t, tt.maxIdleTimeout, pool.maxIdleTime)
			assert.Equal(t, tt.maxConn, cap(pool.conns))
			assert.Equal(t, 0, int(pool.numOpen.Load()))
			assert.False(t, pool.closed)
			assert.Nil(t, pool.cleanerCh)
			assert.Equal(t, int64(0), pool.maxIdleClosed)
			assert.Equal(t, int64(0), pool.maxIdleTimeClosed)
			assert.Equal(t, int64(0), pool.maxLifeTimeClosed)
		})
	}
}

func Test_connPool_get_put(t *testing.T) {

	pool := newConnPool(5, 10, time.Hour, 5*time.Minute, createConn)
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	// start a loop to get and put connections
	wg := sync.WaitGroup{}
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()

			cn, err := pool.get(ctx)
			assert.Nil(t, err)
			assert.NotNil(t, cn)

			time.Sleep(time.Duration(i*100) * time.Millisecond)

			err = pool.put(cn)
			assert.Nil(t, err)
		}(i)
	}

	wg.Wait()

	// idle connections reached maxConn
	assert.Equal(t, 5, int(pool.numOpen.Load()))
	assert.Equal(t, 5, len(pool.conns))
}

// Test_connPool_get_timeout_case1 mocking the case that the createConn function
// takes longer than the context timeout to return a connection.
func Test_connPool_get_timeout_case1(t *testing.T) {

	createConn := func(ctx context.Context) (memcachedConn, error) {
		delay := time.NewTimer(200 * time.Millisecond)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-delay.C:
			// mocking the connection creation process takes 200ms
		}
		return newMockConn(), nil
	}

	pool := newConnPool(5, 10, time.Hour, 5*time.Minute, createConn)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// get a connection, but the createConn function takes 200ms to return
	// so the get function should return an error (get timeout).
	conn, err := pool.get(ctx)
	assert.Error(t, err)
	assert.True(t, errors.Is(err, context.DeadlineExceeded))
	assert.Nil(t, conn)
}

// Test_connPool_get_timeout_case2 mocking the case that the all connections
// are occupied, we must wait for a connection to be put back to the pool but
// no connection is put back before (get) context timeout deadline.
func Test_connPool_get_timeout_case2(t *testing.T) {

	pool := newConnPool(5, 10, time.Hour, 5*time.Minute, createConn)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// run out of max connections
	for i := 0; i < 10; i++ {
		go func() {
			cn, err := pool.get(ctx)
			assert.Nil(t, err)
			assert.NotNil(t, cn)

			time.Sleep(200 * time.Millisecond)
			_ = pool.put(cn)
		}()
	}

	// wait for all connections was gotten.
	time.Sleep(100 * time.Millisecond)

	// get a connection, but the createConn function takes 200ms to return
	// so the get function should return an error (get timeout).
	conn, err := pool.get(ctx)
	assert.Error(t, err)
	assert.Nil(t, conn)
}

func Test_connPool_get_oversize(t *testing.T) {
	pool := newConnPool(5, 10, time.Hour, 5*time.Minute, createConn)

	// run out of max connections
	wg := sync.WaitGroup{}
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			cn, err := pool.get(ctx)
			assert.Nil(t, err)
			assert.NotNil(t, cn)

			time.Sleep(200 * time.Millisecond)
			_ = pool.put(cn)
		}()
	}

	// Make sure all goroutines have been started.
	time.Sleep(100 * time.Millisecond)

	// numOpen connections reached maxConn, now get again
	// should return error (get timeout).
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	cn, err := pool.get(ctx)
	assert.Error(t, err)
	assert.True(t, errors.Is(err, context.DeadlineExceeded))
	assert.Nil(t, cn)

	// wait for all connections to be put back, then get again
	// should return a connection.
	wg.Wait()

	assert.Equal(t, 5, int(pool.numOpen.Load()))
	assert.Equal(t, 5, len(pool.conns))

	ctx, cancel = context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	cn, err = pool.get(ctx)
	assert.Nil(t, err)
	assert.NotNil(t, cn)
}

// Test_connPool_cleanup_maxIdle tests the case that connection sits in
// idle list for more than maxIdleTimeout, it should be closed.
func Test_connPool_cleanup_maxIdle(t *testing.T) {

	// maxLifeTime = 3600s
	// maxIdleTimeout = 1s
	pool := newConnPool(5, 10, time.Hour, time.Second, createConn)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// get 10 connections and put them back, now there are 10 idle connections
	// and 10 numOpen connections.
	for i := 0; i < 10; i++ {
		go func() {
			cn, err := pool.get(ctx)
			assert.Nil(t, err)
			assert.NotNil(t, cn)

			time.Sleep(200 * time.Millisecond)
			_ = pool.put(cn)
		}()
	}

	time.Sleep(100 * time.Millisecond)
	stat := pool.stats()
	t.Logf("Before cleanup: %+v", stat)
	assert.Equal(t, 10, stat.TotalConns)
	assert.Equal(t, 0, stat.IdleConns)
	assert.Equal(t, 10, stat.MaxConns)
	assert.Equal(t, 5, stat.MaxIdle)
	assert.Equal(t, int64(0), stat.maxIdleClosed)
	assert.Equal(t, int64(0), stat.maxLifeTimeClosed)
	assert.Equal(t, int64(0), stat.maxIdleTimeClosed)

	// wait for the cleaner to clean up the idle connection, decrease the numOpen
	// connections to 5.
	time.Sleep(3 * time.Second)
	assert.Equal(t, 0, int(pool.numOpen.Load()))
	assert.Equal(t, 0, len(pool.conns))
	assert.False(t, pool.closed)

	stat = pool.stats()
	t.Logf("After cleanup: %+v", stat)
	assert.Equal(t, 0, stat.TotalConns)
	assert.Equal(t, 0, stat.IdleConns) // since all connections are cleaned up(idle=1s)
	assert.Equal(t, 10, stat.MaxConns)
	assert.Equal(t, 5, stat.MaxIdle)
	assert.Equal(t, int64(5), stat.maxIdleClosed)
	assert.Equal(t, int64(0), stat.maxLifeTimeClosed)
	assert.Equal(t, int64(5), stat.maxIdleTimeClosed)
}

// Test_connPool_cleanup_maxLife tests the case that connection sits in
// idle list for more than maxLifeTime, it should be closed.
func Test_connPool_cleanup_maxLife(t *testing.T) {

	// maxLifeTime = 1s
	// maxIdleTimeout = 3600s
	pool := newConnPool(5, 10, 2*time.Second, time.Hour, createConn)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// get 10 connections and put them back, now there are 10 idle connections
	// and 10 numOpen connections.
	for i := 0; i < 10; i++ {
		go func() {
			cn, err := pool.get(ctx)
			assert.Nil(t, err)
			assert.NotNil(t, cn)

			time.Sleep(200 * time.Millisecond)
			_ = pool.put(cn)
		}()
	}

	time.Sleep(100 * time.Millisecond)
	stat := pool.stats()
	t.Logf("Before cleanup: %+v", stat)
	assert.Equal(t, 10, stat.TotalConns)
	assert.Equal(t, 0, stat.IdleConns)
	assert.Equal(t, 10, stat.MaxConns)
	assert.Equal(t, 5, stat.MaxIdle)
	assert.Equal(t, int64(0), stat.maxIdleClosed)
	assert.Equal(t, int64(0), stat.maxLifeTimeClosed)
	assert.Equal(t, int64(0), stat.maxIdleTimeClosed)

	// wait for the cleaner to clean up the idle connection, decrease the numOpen
	// connections to 5.
	time.Sleep(3 * time.Second)
	assert.Equal(t, 0, int(pool.numOpen.Load()))
	assert.Equal(t, 0, len(pool.conns))
	assert.False(t, pool.closed)

	stat = pool.stats()
	t.Logf("After cleanup: %+v", stat)
	assert.Equal(t, 0, stat.TotalConns)
	assert.Equal(t, 0, stat.IdleConns) // since all connections are cleaned up(idle=1s)
	assert.Equal(t, 10, stat.MaxConns)
	assert.Equal(t, 5, stat.MaxIdle)
	assert.Equal(t, int64(5), stat.maxIdleClosed)
	assert.Equal(t, int64(5), stat.maxLifeTimeClosed)
	assert.Equal(t, int64(0), stat.maxIdleTimeClosed)
}
