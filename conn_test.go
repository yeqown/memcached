package memcached

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"net"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/yeqown/memcached/resolver"
)

func TestNewConnContext(t *testing.T) {
	for _, network := range []string{"tcp", "unix"} {
		t.Run(network, func(t *testing.T) {
			address := "127.0.0.1:0"
			if network == "unix" {
				address = filepath.Join(t.TempDir(), "memcached.sock")
			}
			listener, err := net.Listen(network, address)
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, listener.Close()) })
			done := make(chan error, 1)
			go func() {
				raw, err := listener.Accept()
				if err != nil {
					done <- err
					return
				}
				defer func() { _ = raw.Close() }()
				if err := raw.SetDeadline(time.Now().Add(time.Second)); err != nil {
					done <- err
					return
				}
				request := make([]byte, len("version\r\n"))
				if _, err := io.ReadFull(raw, request); err != nil {
					done <- err
					return
				}
				if string(request) != "version\r\n" {
					done <- io.ErrUnexpectedEOF
					return
				}
				_, err = io.WriteString(raw, "VERSION test\r\n")
				done <- err
			}()
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			cn, err := newConnContext(ctx, resolver.NewAddr(network, listener.Addr().String(), 0), time.Second)
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, cn.Close()) })
			require.NoError(t, cn.setReadDeadline(time.Now().Add(time.Second)))
			require.NoError(t, cn.setWriteDeadline(time.Now().Add(time.Second)))
			_, err = cn.Write([]byte("version\r\n"))
			require.NoError(t, err)
			line, err := cn.readLine('\n')
			require.NoError(t, err)
			require.Equal(t, "VERSION test\r\n", string(line))
			require.NoError(t, <-done)
			require.NoError(t, cn.Close())
			require.NoError(t, cn.Close(), "closing a connection is idempotent")
			_, err = cn.Read(make([]byte, 1))
			require.Error(t, err)
			_, err = cn.Write([]byte("version\r\n"))
			require.Error(t, err)
			_, err = cn.readLine('\n')
			require.Error(t, err)
		})
	}
}

func TestNewConnContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	cn, err := newConnContext(ctx, resolver.NewAddr("tcp", "127.0.0.1:11211", 0), time.Second)
	require.ErrorIs(t, err, context.Canceled)
	require.Nil(t, cn)
}

func TestNewConnContextUDP(t *testing.T) {
	listener, err := net.ListenPacket("udp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, listener.Close()) })
	require.NoError(t, listener.SetDeadline(time.Now().Add(time.Second)))
	done := make(chan error, 1)
	go func() {
		buffer := make([]byte, 64)
		n, address, err := listener.ReadFrom(buffer)
		if err != nil {
			done <- err
			return
		}
		if string(buffer[:n]) != "version\r\n" {
			done <- io.ErrUnexpectedEOF
			return
		}
		_, err = listener.WriteTo([]byte("VERSION test\r\n"), address)
		done <- err
	}()
	cn, err := newConnContext(t.Context(), resolver.NewAddr("udp", listener.LocalAddr().String(), 0), time.Second)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, cn.Close()) })
	require.NoError(t, cn.setReadDeadline(time.Now().Add(time.Second)))
	_, err = cn.Write([]byte("version\r\n"))
	require.NoError(t, err)
	line, err := cn.readLine('\n')
	require.NoError(t, err)
	require.Equal(t, "VERSION test\r\n", string(line))
	require.NoError(t, <-done)
}

func TestConnAgeBoundaries(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		created := time.Now()
		cn := &conn{createdAt: created, returnedAt: created.Add(time.Second)}
		time.Sleep(2 * time.Second)
		for _, test := range []struct {
			name    string
			since   time.Time
			expired bool
		}{
			{name: "disabled"},
			{name: "before creation", since: created.Add(-time.Nanosecond)},
			{name: "at creation", since: created},
			{name: "after creation", since: created.Add(time.Nanosecond), expired: true},
		} {
			age, expired := cn.expired(test.since)
			require.Equal(t, 2*time.Second, age, test.name)
			require.Equal(t, test.expired, expired, test.name)
		}
	})
}

func TestConnIdleBoundaries(t *testing.T) {
	returned := time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC)
	cn := &conn{returnedAt: returned}
	for _, test := range []struct {
		name      string
		since     time.Time
		idle      bool
		remaining time.Duration
	}{
		{name: "disabled", remaining: returned.Sub(zeroTime)},
		{name: "before return", since: returned.Add(-time.Second), remaining: time.Second},
		{name: "at return", since: returned},
		{name: "after return", since: returned.Add(time.Nanosecond), idle: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			remaining, idle := cn.idle(test.since)
			require.Equal(t, test.remaining, remaining)
			require.Equal(t, test.idle, idle)
		})
	}
}

// testConn replaces transport I/O while keeping conn's age and idle checks.
// Each write supplies a fresh response so a returned connection can be reused.
type testConn struct {
	*conn
	ioMu          sync.Mutex
	reader        *bufio.Reader
	response      []byte
	writes        [][]byte
	respond       func([]byte) ([]byte, error)
	readErr       error
	writeErr      error
	readDeadline  time.Time
	writeDeadline time.Time
	closes        atomic.Int32
	onClose       func()
	closeErr      error
}

var _ memcachedConn = (*testConn)(nil)

func newTestConn(response ...[]byte) *testConn {
	data := bytes.Join(response, nil)
	return &testConn{
		conn:     &conn{createdAt: nowFunc(), returnedAt: nowFunc()},
		response: data,
		reader:   bufio.NewReader(bytes.NewReader(data)),
	}
}

func (c *testConn) Write(p []byte) (int, error) {
	c.ioMu.Lock()
	c.writes = append(c.writes, bytes.Clone(p))
	c.ioMu.Unlock()
	if c.writeErr != nil {
		return 0, c.writeErr
	}
	data := c.response
	if c.respond != nil {
		var err error
		data, err = c.respond(p)
		if err != nil {
			return 0, err
		}
	}
	c.ioMu.Lock()
	c.reader.Reset(bytes.NewReader(data))
	c.ioMu.Unlock()
	return len(p), nil
}

func (c *testConn) Read(p []byte) (int, error) {
	c.ioMu.Lock()
	defer c.ioMu.Unlock()
	if c.readErr != nil {
		return 0, c.readErr
	}
	return c.reader.Read(p)
}

func (c *testConn) readLine(delim byte) ([]byte, error) {
	c.ioMu.Lock()
	defer c.ioMu.Unlock()
	if c.readErr != nil {
		return nil, c.readErr
	}
	return c.reader.ReadBytes(delim)
}

func (c *testConn) Close() error {
	c.closes.Add(1)
	if c.onClose != nil {
		c.onClose()
	}
	return c.closeErr
}

func (c *testConn) setReadDeadline(d time.Time) error {
	c.readDeadline = d
	return nil
}

func (c *testConn) setWriteDeadline(d time.Time) error {
	c.writeDeadline = d
	return nil
}

func (c *testConn) release() error {
	_ = c.setReadDeadline(zeroTime)
	_ = c.setWriteDeadline(zeroTime)
	c.returnedAt = nowFunc()
	return c.pool.put(c)
}

func createTestConn(context.Context) (memcachedConn, error) { return newTestConn(), nil }
