package memcached

import (
	"context"
	"time"
)

var _ memcachedConn = (*mockConn)(nil)

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
