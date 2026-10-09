package resolver

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/iotest"
	"time"

	"github.com/stretchr/testify/require"
)

// configServer accepts repeated requests; a handler error closes the connection.
func configServer(t *testing.T, respond func(net.Conn, int32) error) (string, *atomic.Int32) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	var accepted, requests atomic.Int32
	var connections sync.Map
	var handlers sync.WaitGroup
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			accepted.Add(1)
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
						t.Errorf("unexpected command %q", command)
						return
					}
					if err := respond(conn, requests.Add(1)); err != nil {
						return
					}
				}
			}()
		}
	}()
	t.Cleanup(func() {
		_ = listener.Close()
		<-done
		connections.Range(func(key, _ any) bool { _ = key.(net.Conn).Close(); return true })
		handlers.Wait()
	})
	return listener.Addr().String(), &accepted
}

func waitConfigSignal(t *testing.T, signal <-chan struct{}) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(time.Second):
		t.Fatal("discovery did not complete the expected operation")
	}
}

func configResponse(version, nodes string) string {
	payload := version + "\n" + nodes + "\n"
	return fmt.Sprintf("CONFIG cluster 0 %d\r\n%s\r\nEND\r\n", len(payload), payload)
}

// discoveryConn supplies one fixed exchange without opening a socket.
type discoveryConn struct {
	reader   io.Reader
	writes   bytes.Buffer
	writeErr error
	deadline time.Time
	closes   int
}

func (c *discoveryConn) Read(p []byte) (int, error) { return c.reader.Read(p) }

func (c *discoveryConn) Write(p []byte) (int, error) {
	if c.writeErr != nil {
		return 0, c.writeErr
	}
	return c.writes.Write(p)
}

func (c *discoveryConn) Close() error { c.closes++; return nil }

func (*discoveryConn) LocalAddr() net.Addr { return &net.TCPAddr{} }

func (*discoveryConn) RemoteAddr() net.Addr { return &net.TCPAddr{} }

func (c *discoveryConn) SetDeadline(d time.Time) error {
	c.deadline = d
	return nil
}

func (c *discoveryConn) SetReadDeadline(d time.Time) error {
	return c.SetDeadline(d)
}

func (c *discoveryConn) SetWriteDeadline(d time.Time) error {
	return c.SetDeadline(d)
}

func TestAutoDiscoveryResolve(t *testing.T) {
	for _, test := range []struct {
		name     string
		interval time.Duration
		wantWait time.Duration
	}{
		{"custom interval", 25 * time.Millisecond, 25 * time.Millisecond},
		{"default interval", 0, time.Minute},
	} {
		t.Run(test.name, func(t *testing.T) {
			conn := &discoveryConn{reader: strings.NewReader(configResponse("13", "cache.example|10.0.0.1|11211 |2001:db8::1|11212"))}
			r := NewAutoDiscovery(test.interval)
			r.conn = conn
			t.Cleanup(func() { require.NoError(t, r.Close()) })
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()

			start := time.Now()
			result, next, err := r.Resolve(ctx, "directory:11211")
			require.NoError(t, err)
			require.Equal(t, ResolveResult{Generation: 13, Addrs: []*Addr{
				NewAddr("tcp", "10.0.0.1:11211", 0), NewAddr("tcp", "[2001:db8::1]:11212", 0),
			}}, result)
			require.NotNil(t, next)
			require.False(t, next.Before(start.Add(test.wantWait)))
			require.False(t, next.After(time.Now().Add(test.wantWait)))
			deadline, ok := ctx.Deadline()
			require.True(t, ok)
			require.Equal(t, deadline, conn.deadline)
			require.Equal(t, "config get cluster\r\n", conn.writes.String())
		})
	}
}

func TestAutoDiscoveryConnectionLifecycle(t *testing.T) {
	target, accepted := configServer(t, func(conn net.Conn, request int32) error {
		if request == 3 {
			return io.EOF
		}
		_, err := io.WriteString(conn, configResponse(fmt.Sprint(request), "cache.example|10.0.0.1|11211"))
		return err
	})
	r := NewAutoDiscovery(time.Minute)
	t.Cleanup(func() { require.NoError(t, r.Close()) })
	for _, generation := range []uint64{1, 2} {
		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		result, _, err := r.Resolve(ctx, "tcp://"+target)
		cancel() // A completed request's cancellation must not poison reuse.
		require.NoError(t, err)
		require.Equal(t, generation, result.Generation)
	}
	require.EqualValues(t, 1, accepted.Load())
	_, _, err := r.Resolve(t.Context(), target)
	require.ErrorIs(t, err, io.ErrUnexpectedEOF)
	require.EqualValues(t, 1, accepted.Load(), "a failed exchange must not retry immediately")
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	result, _, err := r.Resolve(ctx, target)
	require.NoError(t, err)
	require.Equal(t, uint64(4), result.Generation)
	require.EqualValues(t, 2, accepted.Load(), "the next attempt must reconnect")
	require.NoError(t, r.Close())
	require.NoError(t, r.Close())
	_, _, err = r.Resolve(t.Context(), target)
	require.ErrorIs(t, err, net.ErrClosed)
}

func TestAutoDiscoveryInvalidTarget(t *testing.T) {
	for _, test := range []struct {
		target  string
		wantErr error
	}{
		{"", ErrInvalidAddress},
		{"udp://127.0.0.1:11211", ErrInvalidNetworkProtocol},
	} {
		t.Run(test.target, func(t *testing.T) {
			r := NewAutoDiscovery(time.Second)
			t.Cleanup(func() { require.NoError(t, r.Close()) })
			result, next, err := r.Resolve(t.Context(), test.target)
			require.ErrorIs(t, err, test.wantErr)
			require.Empty(t, result)
			require.NotNil(t, next)
		})
	}
}

func TestAutoDiscoveryCloseAlreadyClosedConnection(t *testing.T) {
	target, _ := configServer(t, func(net.Conn, int32) error { return nil })
	conn, err := (&net.Dialer{}).DialContext(t.Context(), "tcp", target)
	require.NoError(t, err)
	r := NewAutoDiscovery(time.Minute)
	r.conn = conn
	t.Cleanup(func() { require.NoError(t, r.Close()) })

	// A cancellation callback can close the socket before Close detaches it.
	require.NoError(t, conn.Close())
	require.NoError(t, r.Close())
	_, _, err = r.Resolve(t.Context(), target)
	require.ErrorIs(t, err, net.ErrClosed)
}

func TestAutoDiscoveryFailure(t *testing.T) {
	readErr, writeErr := errors.New("read failed"), errors.New("write failed")
	for _, test := range []struct {
		name       string
		reader     io.Reader
		writeErr   error
		wantErr    error
		wantCached bool
	}{
		{"timeout uses cache", iotest.ErrReader(context.DeadlineExceeded), nil, nil, true},
		{"EOF", strings.NewReader(""), nil, io.ErrUnexpectedEOF, false},
		{"read failure", iotest.ErrReader(readErr), nil, readErr, false},
		{"write failure", strings.NewReader(""), writeErr, writeErr, false},
		{"server error", strings.NewReader("SERVER_ERROR unavailable\r\n"), nil, ErrMalformedResponse, false},
		{"no config", strings.NewReader("END\r\n"), nil, ErrNoClusterConfig, false},
		{"invalid config", strings.NewReader(configResponse("invalid", "cache.example|10.0.0.1|11211")), nil, ErrMalformedResponse, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			cached := ResolveResult{Generation: 12, Addrs: []*Addr{NewAddr("tcp", "10.0.0.1:11211", 0)}}
			conn := &discoveryConn{reader: test.reader, writeErr: test.writeErr}
			r := NewAutoDiscovery(time.Minute)
			r.conn, r.cached = conn, cached
			t.Cleanup(func() { require.NoError(t, r.Close()) })

			result, next, err := r.Resolve(t.Context(), "directory:11211")
			require.NotNil(t, next, "failed attempts must schedule another resolve")
			if test.wantCached {
				require.NoError(t, err)
				require.Equal(t, cached, result)
			} else {
				require.ErrorIs(t, err, test.wantErr)
				require.Empty(t, result)
			}
			require.Equal(t, cached, r.cached, "failed exchanges must preserve the last complete config")
			require.Nil(t, r.conn, "a failed exchange must discard its connection")
			require.Equal(t, 1, conn.closes)
			if test.writeErr == nil {
				require.Equal(t, "config get cluster\r\n", conn.writes.String())
			}
		})
	}
}

func TestAutoDiscoveryGeneration(t *testing.T) {
	cached := ResolveResult{Generation: 12, Addrs: []*Addr{NewAddr("tcp", "10.0.0.1:11211", 0)}}
	updated := ResolveResult{Generation: 13, Addrs: []*Addr{NewAddr("tcp", "10.0.0.2:11211", 0)}}
	for _, test := range []struct {
		name       string
		generation uint64
		want       ResolveResult
		wantErr    error
	}{
		{"stale", 11, cached, ErrStaleConfig},
		{"equal", 12, cached, nil},
		{"newer", 13, updated, nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			wire := configResponse(fmt.Sprint(test.generation), "cache.example|10.0.0.2|11211")
			conn := &discoveryConn{reader: strings.NewReader(wire)}
			r := NewAutoDiscovery(time.Minute)
			r.conn, r.cached = conn, cached
			t.Cleanup(func() { require.NoError(t, r.Close()) })

			result, next, err := r.Resolve(t.Context(), "directory:11211")
			require.NotNil(t, next)
			if test.wantErr != nil {
				require.ErrorIs(t, err, test.wantErr)
				require.Empty(t, result)
			} else {
				require.NoError(t, err)
				require.Equal(t, test.want, result)
			}
			require.Equal(t, test.want, r.cached)
			require.Same(t, conn, r.conn, "complete replies leave the connection reusable")
			require.Zero(t, conn.closes)
		})
	}
}

func TestAutoDiscoveryCacheOwnership(t *testing.T) {
	for _, cached := range []bool{false, true} {
		t.Run(fmt.Sprintf("cached=%t", cached), func(t *testing.T) {
			want := NewAddr("tcp", "10.0.0.1:11211", 0)
			r := NewAutoDiscovery(time.Minute)
			t.Cleanup(func() { require.NoError(t, r.Close()) })
			var reader io.Reader = strings.NewReader(configResponse("12", "cache.example|10.0.0.1|11211"))
			if cached {
				r.cached = ResolveResult{Generation: 12, Addrs: []*Addr{want.Clone()}}
				reader = iotest.ErrReader(context.DeadlineExceeded)
			}
			r.conn = &discoveryConn{reader: reader}

			result, _, err := r.Resolve(t.Context(), "directory:11211")
			require.NoError(t, err)
			require.Len(t, result.Addrs, 1)
			result.Addrs[0].Address = "caller.example:11211"
			result.Addrs[0].Add("caller", true)
			result.Addrs[0] = NewAddr("tcp", "replacement.example:11211", 0)
			require.Equal(t, ResolveResult{Generation: 12, Addrs: []*Addr{want}}, r.cached)
		})
	}
}

func TestAutoDiscoveryTimeoutWithoutCache(t *testing.T) {
	conn := &discoveryConn{reader: iotest.ErrReader(context.DeadlineExceeded)}
	r := NewAutoDiscovery(time.Minute)
	r.conn = conn
	t.Cleanup(func() { require.NoError(t, r.Close()) })

	result, next, err := r.Resolve(t.Context(), "directory:11211")
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Empty(t, result)
	require.NotNil(t, next)
	require.Nil(t, r.conn)
	require.Equal(t, 1, conn.closes)
}

func TestAutoDiscoveryClearsPreviousDeadline(t *testing.T) {
	conn := &discoveryConn{reader: strings.NewReader(configResponse("1", "cache.example|10.0.0.1|11211"))}
	r := NewAutoDiscovery(time.Minute)
	r.conn = conn
	t.Cleanup(func() { require.NoError(t, r.Close()) })
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	_, _, err := r.Resolve(ctx, "directory:11211")
	require.NoError(t, err)
	require.False(t, conn.deadline.IsZero())

	conn.reader = strings.NewReader(configResponse("2", "cache.example|10.0.0.2|11211"))
	_, _, err = r.Resolve(t.Context(), "directory:11211")
	require.NoError(t, err)
	require.True(t, conn.deadline.IsZero(), "a reused connection must use the current request's deadline")
}

func TestAutoDiscoveryInterruption(t *testing.T) {
	for _, operation := range []string{"cancel", "close"} {
		t.Run(operation, func(t *testing.T) {
			conn, peer := net.Pipe()
			t.Cleanup(func() { require.NoError(t, peer.Close()) })
			r := NewAutoDiscovery(time.Minute)
			r.conn = conn
			r.cached = ResolveResult{Generation: 1, Addrs: []*Addr{NewAddr("tcp", "10.0.0.1:11211", 0)}}
			t.Cleanup(func() { require.NoError(t, r.Close()) })
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			var result ResolveResult
			var next *time.Time
			var err error
			finished := make(chan struct{})
			go func() {
				result, next, err = r.Resolve(ctx, "directory:11211")
				close(finished)
			}()
			require.NoError(t, peer.SetDeadline(time.Now().Add(time.Second)))
			command, readErr := bufio.NewReader(peer).ReadString('\n')
			require.NoError(t, readErr)
			require.Equal(t, "config get cluster\r\n", command)

			wantErr := context.Canceled
			if operation == "close" {
				require.NoError(t, r.Close())
				wantErr = net.ErrClosed
			} else {
				cancel()
			}
			waitConfigSignal(t, finished)
			require.ErrorIs(t, err, wantErr)
			require.Empty(t, result, "cancellation must not fall back to cached data")
			require.NotNil(t, next)
			_, readErr = peer.Read(make([]byte, 1))
			require.ErrorIs(t, readErr, io.EOF, "the interrupted connection must be closed")
		})
	}
}

func TestReadConfigResponse(t *testing.T) {
	for _, test := range []struct {
		name, wire string
		generation uint64
		addresses  []string
	}{
		{
			"AWS",
			configResponse("12", "cache-1.example|10.0.0.1|11211 cache-2.example||11212"),
			12,
			[]string{"10.0.0.1:11211", "cache-2.example:11212"},
		},
		{
			"Google IPv6",
			configResponse("2", "10.0.0.1|10.0.0.1|11211 |2001:db8::1|11212"),
			2,
			[]string{"10.0.0.1:11211", "[2001:db8::1]:11212"},
		},
		{
			"CRLF framing",
			configResponse("1", "cache.example|127.0.0.1|11211"),
			1,
			[]string{"127.0.0.1:11211"},
		},
		{
			"LF",
			strings.ReplaceAll(configResponse("1", "cache.example|127.0.0.1|11211"), "\r\n", "\n"),
			1,
			[]string{"127.0.0.1:11211"},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			reader := bufio.NewReader(strings.NewReader(test.wire))
			result, err := readConfigResponse(t.Context(), reader)
			require.NoError(t, err)
			require.Equal(t, test.generation, result.Generation)
			require.Len(t, result.Addrs, len(test.addresses))
			for i, address := range test.addresses {
				require.Equal(t, NewAddr("tcp", address, 0), result.Addrs[i])
			}
		})
	}
}

func TestReadConfigResponseInvalid(t *testing.T) {
	for _, test := range []struct {
		name, wire string
	}{
		{"empty stream", ""},
		{"server error", "SERVER_ERROR config unavailable\r\n"},
		{"unknown command", "ERROR\r\n"},
		{"wrong key", strings.Replace(configResponse("1", "cache.example|10.0.0.1|11211"), "CONFIG cluster", "CONFIG other", 1)},
		{"wrong flags", strings.Replace(configResponse("1", "cache.example|10.0.0.1|11211"), "cluster 0", "cluster 1", 1)},
		{"bad length", "CONFIG cluster 0 nope\r\n"},
		{"empty body", "CONFIG cluster 0 0\r\n\r\nEND\r\n"},
		{"truncated body", "CONFIG cluster 0 40\r\n1\ncache"},
		{"missing END", strings.TrimSuffix(configResponse("1", "cache.example|10.0.0.1|11211"), "END\r\n")},
		{"wrong terminator", strings.TrimSuffix(configResponse("1", "cache.example|10.0.0.1|11211"), "END\r\n") + "ENDinvalid\r\n"},
		{"bad generation", configResponse("v1", "cache.example||11211")},
		{"empty nodes", configResponse("1", "")},
		{"bad node fields", configResponse("1", "cache.example|11211")},
		{"extra node field", configResponse("1", "cache.example|10.0.0.1|11211|extra")},
		{"empty address", configResponse("1", "||11211")},
		{"bad port", configResponse("1", "cache.example|10.0.0.1|invalid")},
	} {
		t.Run(test.name, func(t *testing.T) {
			result, err := readConfigResponse(t.Context(), bufio.NewReader(strings.NewReader(test.wire)))
			require.ErrorIs(t, err, ErrMalformedResponse)
			require.Empty(t, result)
		})
	}
}

func TestReadConfigResponseReadError(t *testing.T) {
	readErr := errors.New("read failed")
	for _, test := range []struct {
		name, prefix string
	}{
		{"header", ""},
		{"generation", "CONFIG cluster 0 31\r\n"},
		{"nodes", "CONFIG cluster 0 31\r\n1\n"},
		{"terminator", "CONFIG cluster 0 31\r\n1\ncache.example|10.0.0.1|11211\n\r\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			reader := io.MultiReader(strings.NewReader(test.prefix), iotest.ErrReader(readErr))
			result, err := readConfigResponse(t.Context(), bufio.NewReader(reader))
			require.ErrorIs(t, err, readErr)
			require.Empty(t, result)
		})
	}
}

func TestReadConfigResponseCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	result, err := readConfigResponse(ctx, bufio.NewReader(strings.NewReader("")))
	require.ErrorIs(t, err, context.Canceled)
	require.Empty(t, result)
}

func TestReadConfigResponseStopsAtEND(t *testing.T) {
	input, output := io.Pipe()
	t.Cleanup(func() {
		require.NoError(t, input.Close())
		require.NoError(t, output.Close())
	})
	var result ResolveResult
	var err error
	finished := make(chan struct{})
	go func() {
		result, err = readConfigResponse(t.Context(), bufio.NewReader(input))
		close(finished)
	}()
	_, writeErr := io.WriteString(output, configResponse("1", "cache.example|127.0.0.1|11211"))
	require.NoError(t, writeErr)
	waitConfigSignal(t, finished)
	require.NoError(t, err)
	require.Equal(t, uint64(1), result.Generation)
	require.Len(t, result.Addrs, 1)
	require.Equal(t, NewAddr("tcp", "127.0.0.1:11211", 0), result.Addrs[0])
}
