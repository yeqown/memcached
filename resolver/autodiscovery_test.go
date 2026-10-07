package resolver

import (
	"bufio"
	"context"
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

func configResponse(payload string) string {
	return fmt.Sprintf("CONFIG cluster 0 %d\r\n%s\r\nEND\r\n", len(payload), payload)
}

func TestAutoDiscoveryResolve(t *testing.T) {
	for _, test := range []struct {
		name     string
		resolver *AutoDiscovery
		interval time.Duration
	}{
		{"custom interval", NewAutoDiscovery(25 * time.Millisecond), 25 * time.Millisecond},
		{"default interval", NewAutoDiscovery(0), time.Minute},
		{"zero value", &AutoDiscovery{}, time.Minute},
	} {
		t.Run(test.name, func(t *testing.T) {
			target, accepted := configServer(t, func(conn net.Conn, request int32) error {
				_, err := io.WriteString(conn, configResponse(fmt.Sprintf("%d\ncache.example|10.0.0.1|11211 |2001:db8::1|11212\n", request+12)))
				return err
			})
			r := test.resolver
			t.Cleanup(func() { require.NoError(t, r.Close()) })
			for _, generation := range []uint64{13, 14, 15} {
				ctx, cancel := context.WithTimeout(t.Context(), time.Second)
				start := time.Now()
				result, next, err := r.Resolve(ctx, "tcp://"+target)
				cancel() // Canceling a completed request must leave the connection reusable.
				require.NoError(t, err)
				require.Equal(t, generation, result.Generation)
				require.Len(t, result.Addrs, 2)
				require.Equal(t, "10.0.0.1:11211", result.Addrs[0].Address)
				require.Equal(t, "[2001:db8::1]:11212", result.Addrs[1].Address)
				require.NotNil(t, next)
				require.False(t, next.Before(start.Add(test.interval)))
				require.False(t, next.After(time.Now().Add(test.interval)))
			}
			require.EqualValues(t, 1, accepted.Load())
			require.NoError(t, r.Close())
			require.NoError(t, r.Close())
			_, _, err := r.Resolve(t.Context(), target)
			require.ErrorIs(t, err, net.ErrClosed)
		})
	}
}

func TestAutoDiscoveryInvalidTarget(t *testing.T) {
	for _, test := range []struct {
		target  string
		wantErr error
	}{
		{"bad#host:11211", ErrInvalidAddress},
		{"udp://127.0.0.1:11211", ErrInvalidNetworkProtocol},
		{"unix:///tmp/cache.sock", ErrInvalidNetworkProtocol},
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

func TestAutoDiscoveryFailure(t *testing.T) {
	for _, test := range []struct {
		name, wire          string
		timeout, eof, reset bool
		wantErr             error
	}{
		{name: "partial response timeout", wire: "CONFIG cluster 0 80\r\n99\r\nlate", timeout: true},
		{name: "EOF", eof: true, wantErr: io.ErrUnexpectedEOF},
		{name: "reset", eof: true, reset: true},
		{name: "server error", wire: "SERVER_ERROR unavailable\r\n", wantErr: ErrMalformedResponse},
		{name: "no config", wire: "END\r\n", wantErr: ErrNoClusterConfig},
		{name: "invalid config", wire: configResponse("invalid\nother.example||11211\n"), wantErr: ErrMalformedResponse},
	} {
		t.Run(test.name, func(t *testing.T) {
			disconnected := make(chan struct{})
			target, accepted := configServer(t, func(conn net.Conn, request int32) error {
				switch request {
				case 1:
					_, err := io.WriteString(conn, configResponse("12\ncurrent.example||11211\n"))
					return err
				case 2:
					if test.eof {
						if test.reset {
							if err := conn.(*net.TCPConn).SetLinger(0); err != nil {
								return err
							}
						}
						return io.EOF
					}
					if _, err := io.WriteString(conn, test.wire); err != nil {
						return err
					}
					if !test.timeout {
						return nil
					}
					_, err := io.Copy(io.Discard, conn)
					close(disconnected)
					return err
				case 3:
					_, err := io.Copy(io.Discard, conn)
					return err
				default:
					_, err := io.WriteString(conn, configResponse("13\nnext.example||11211\n"))
					return err
				}
			})
			r := NewAutoDiscovery(time.Minute)
			t.Cleanup(func() { require.NoError(t, r.Close()) })
			result, _, err := r.Resolve(t.Context(), target)
			require.NoError(t, err)
			if test.timeout {
				// Check cache ownership once, for both successful and cached results.
				result.Addrs[0].Address = "caller.example:11211"
				result.Addrs[0].Add("caller", true)
				result.Addrs[0] = NewAddr("tcp", "replacement.example:11211", 0)
			}

			timeout := 2 * time.Second
			if test.timeout {
				timeout = 50 * time.Millisecond
			}
			ctx, cancel := context.WithTimeout(t.Context(), timeout)
			result, next, err := r.Resolve(ctx, target)
			cancel()
			require.NotNil(t, next)
			if test.timeout {
				require.NoError(t, err)
				require.Equal(t, uint64(12), result.Generation)
				require.Len(t, result.Addrs, 1)
				require.Equal(t, "current.example:11211", result.Addrs[0].Address)
				require.Nil(t, result.Addrs[0].GetMetadata("caller"))
				result.Addrs[0].Address = "cached-caller.example:11211"
				result.Addrs[0].Add("caller", true)
				result.Addrs[0] = NewAddr("tcp", "cached-replacement.example:11211", 0)
				waitConfigSignal(t, disconnected)
			} else {
				require.Error(t, err)
				if test.wantErr != nil {
					require.ErrorIs(t, err, test.wantErr)
				}
				require.Empty(t, result)
			}
			require.EqualValues(t, 1, accepted.Load(), "a failed exchange must not reconnect immediately")

			// Every failure must preserve the last successful config for a later timeout.
			ctx, cancel = context.WithTimeout(t.Context(), 50*time.Millisecond)
			result, _, err = r.Resolve(ctx, target)
			cancel()
			require.NoError(t, err)
			require.Equal(t, uint64(12), result.Generation)
			require.Len(t, result.Addrs, 1)
			require.Equal(t, "current.example:11211", result.Addrs[0].Address)
			if test.timeout {
				require.Nil(t, result.Addrs[0].GetMetadata("caller"))
			}
			require.EqualValues(t, 2, accepted.Load())

			result, _, err = r.Resolve(t.Context(), target)
			require.NoError(t, err)
			require.Equal(t, uint64(13), result.Generation)
			require.Equal(t, "next.example:11211", result.Addrs[0].Address)
			require.EqualValues(t, 3, accepted.Load())
		})
	}
}

func TestAutoDiscoveryGeneration(t *testing.T) {
	configs := []string{
		"12\ncurrent.example||11211\n",
		"11\nolder.example||11211\n",
		"12\nother.example||11211\n",
		"", // Time out before the next version can replace the cache.
		"13\nnext.example||11211\n",
	}
	target, accepted := configServer(t, func(conn net.Conn, request int32) error {
		if int(request) > len(configs) || configs[request-1] == "" {
			_, err := io.Copy(io.Discard, conn)
			return err
		}
		_, err := io.WriteString(conn, configResponse(configs[request-1]))
		return err
	})
	r := NewAutoDiscovery(time.Minute)
	t.Cleanup(func() { require.NoError(t, r.Close()) })
	for _, test := range []struct {
		generation uint64
		address    string
		wantErr    error
		timeout    bool
	}{
		{12, "current.example:11211", nil, false},
		{0, "", ErrStaleConfig, false},
		{12, "current.example:11211", nil, false},
		{12, "current.example:11211", nil, true},
		{13, "next.example:11211", nil, false},
	} {
		ctx, cancel := context.WithCancel(t.Context())
		if test.timeout {
			require.EqualValues(t, 1, accepted.Load(), "complete stale and equal replies leave the connection reusable")
			cancel()
			ctx, cancel = context.WithTimeout(t.Context(), 50*time.Millisecond)
		}
		result, next, err := r.Resolve(ctx, target)
		cancel()
		require.NotNil(t, next)
		if test.wantErr != nil {
			require.ErrorIs(t, err, test.wantErr)
			require.Empty(t, result)
			continue
		}
		require.NoError(t, err)
		require.Equal(t, test.generation, result.Generation)
		require.Equal(t, test.address, result.Addrs[0].Address)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	result, _, err := r.Resolve(ctx, target)
	require.NoError(t, err)
	require.Equal(t, uint64(13), result.Generation)
	require.Equal(t, "next.example:11211", result.Addrs[0].Address)
	require.EqualValues(t, 2, accepted.Load())
}

func TestAutoDiscoveryTargetChange(t *testing.T) {
	disconnected := make(chan struct{})
	first, _ := configServer(t, func(conn net.Conn, _ int32) error {
		if _, err := io.WriteString(conn, configResponse("12\nfirst.example||11211\n")); err != nil {
			return err
		}
		_, err := io.Copy(io.Discard, conn)
		close(disconnected)
		return err
	})
	second, _ := configServer(t, func(conn net.Conn, request int32) error {
		if request == 1 {
			_, err := io.Copy(io.Discard, conn)
			return err
		}
		_, err := io.WriteString(conn, configResponse("1\nsecond.example||11211\n"))
		return err
	})
	r := NewAutoDiscovery(time.Minute)
	t.Cleanup(func() { require.NoError(t, r.Close()) })
	_, _, err := r.Resolve(t.Context(), first)
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	result, _, err := r.Resolve(ctx, second)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Empty(t, result, "another endpoint's cache must never be returned")
	waitConfigSignal(t, disconnected)
	result, _, err = r.Resolve(t.Context(), second)
	require.NoError(t, err)
	require.Equal(t, uint64(1), result.Generation, "the new target has its own generation")
	require.Equal(t, "second.example:11211", result.Addrs[0].Address)
}

func TestAutoDiscoveryInterruption(t *testing.T) {
	for _, test := range []struct {
		name   string
		cached bool
	}{
		{"cancel", false},
		{"cancel", true},
		{"close", false},
		{"close", true},
		{"timeout", false},
	} {
		t.Run(fmt.Sprintf("%s/cache=%t", test.name, test.cached), func(t *testing.T) {
			entered, disconnected := make(chan struct{}), make(chan struct{})
			target, _ := configServer(t, func(conn net.Conn, request int32) error {
				if test.cached && request == 1 {
					_, err := io.WriteString(conn, configResponse("1\ncache.example||11211\n"))
					return err
				}
				close(entered)
				_, err := io.Copy(io.Discard, conn)
				close(disconnected)
				return err
			})
			r := NewAutoDiscovery(time.Minute)
			t.Cleanup(func() { require.NoError(t, r.Close()) })
			if test.cached {
				_, _, err := r.Resolve(t.Context(), target)
				require.NoError(t, err)
			}
			ctx, cancel := context.WithCancel(t.Context())
			if test.name == "timeout" {
				cancel()
				ctx, cancel = context.WithTimeout(t.Context(), 50*time.Millisecond)
			}
			defer cancel()
			var result ResolveResult
			var next *time.Time
			var err error
			finished := make(chan struct{})
			go func() {
				result, next, err = r.Resolve(ctx, target)
				close(finished)
			}()
			waitConfigSignal(t, entered)
			wantErr := context.DeadlineExceeded
			switch test.name {
			case "cancel":
				cancel()
				wantErr = context.Canceled
			case "close":
				require.NoError(t, r.Close())
				wantErr = net.ErrClosed
			}
			waitConfigSignal(t, finished)
			waitConfigSignal(t, disconnected)
			require.ErrorIs(t, err, wantErr)
			require.Empty(t, result)
			require.NotNil(t, next)
		})
	}
}

func TestAutoDiscoveryConcurrentRequests(t *testing.T) {
	target, accepted := configServer(t, func(conn net.Conn, request int32) error {
		_, err := io.WriteString(conn, configResponse(fmt.Sprintf("%d\ncache.example||11211\n", request)))
		return err
	})
	r := NewAutoDiscovery(time.Minute)
	t.Cleanup(func() { require.NoError(t, r.Close()) })
	const requests = 20
	type resolveResult struct {
		config ResolveResult
		err    error
	}
	results := make(chan resolveResult, requests)
	for range requests {
		go func() {
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			config, _, err := r.Resolve(ctx, target)
			results <- resolveResult{config, err}
		}()
	}
	seen := make(map[uint64]bool)
	for range requests {
		result := <-results
		require.NoError(t, result.err)
		require.Len(t, result.config.Addrs, 1)
		seen[result.config.Generation] = true
	}
	require.Len(t, seen, requests, "each request must consume exactly its own response")
	require.EqualValues(t, 1, accepted.Load())
}

func TestReadConfigResponse(t *testing.T) {
	for _, test := range []struct {
		name, wire string
		generation uint64
		addresses  []string
	}{
		{"AWS", configResponse("12\nCACHE-1.EXAMPLE.|10.0.0.1|11211 cache-2.example||011212\n"), 12, []string{"10.0.0.1:11211", "cache-2.example:11212"}},
		{"duplicate identity", configResponse("3\ncache.example|127.0.0.1|11211 cache.example|::ffff:127.0.0.1|011211\n"), 3, []string{"127.0.0.1:11211"}},
		{"Google IPv6", configResponse("2\n10.0.0.1|10.0.0.1|11211 |2001:db8::1|11212\n"), 2, []string{"10.0.0.1:11211", "[2001:db8::1]:11212"}},
		{"uint64 generation", configResponse("18446744073709551615\ncache.example|::ffff:127.0.0.1|11211\n"), 18446744073709551615, []string{"127.0.0.1:11211"}},
		{"CRLF", "CONFIG cluster 0 80\r\n1\r\ncache.example|127.0.0.1|11211\n\r\nEND\r\n", 1, []string{"127.0.0.1:11211"}},
		{"LF", "CONFIG cluster 0 80\n1\ncache.example|127.0.0.1|11211\nEND\n", 1, []string{"127.0.0.1:11211"}},
		{"whitespace", "\r\n  CONFIG cluster 0 80  \r\n\r\n  1  \r\n  cache.example|127.0.0.1|11211  \r\n\r\nEND\r\n", 1, []string{"127.0.0.1:11211"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			scanner := newConfigScanner(iotest.OneByteReader(strings.NewReader(test.wire)))
			result, err := readConfigResponse(scanner)
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
	valid := configResponse("1\ncache.example|10.0.0.1|11211\n")
	for name, wire := range map[string]string{
		"server error":        "SERVER_ERROR config unavailable\r\n",
		"unknown command":     "ERROR\r\n",
		"legacy VALUE":        "VALUE AmazonElastiCache:cluster 0 4\r\n1\na\n\r\nEND\r\n",
		"wrong key":           strings.Replace(valid, "CONFIG cluster", "CONFIG other", 1),
		"wrong flags":         strings.Replace(valid, "cluster 0", "cluster 1", 1),
		"bad length":          "CONFIG cluster 0 nope\r\n",
		"empty body":          "CONFIG cluster 0 0\r\n\r\nEND\r\n",
		"oversized body":      "CONFIG cluster 0 1048577\r\n",
		"oversized header":    strings.Repeat(" ", maxConfigHeaderBytes+1) + valid,
		"oversized line":      strings.Repeat("X", maxConfigBytes+1),
		"truncated body":      "CONFIG cluster 0 40\r\n1\ncache",
		"missing END":         strings.TrimSuffix(valid, "END\r\n"),
		"wrong terminator":    strings.TrimSuffix(valid, "END\r\n") + "ENDinvalid\r\n",
		"generation overflow": configResponse("18446744073709551616\ncache.example||11211\n"),
		"bad generation":      configResponse("v1\ncache.example||11211\n"),
		"empty nodes":         configResponse("1\n\n"),
		"extra line":          configResponse("1\ncache.example||11211\nextra\n"),
		"bad node fields":     configResponse("1\ncache.example|11211\n"),
		"extra node field":    configResponse("1\ncache.example|10.0.0.1|11211|extra\n"),
		"empty address":       configResponse("1\n||11211\n"),
		"bad hostname":        configResponse("1\nbad#host||11211\n"),
		"bad IP":              configResponse("1\ncache.example|999.999.999.999|11211\n"),
		"hostname as IP":      configResponse("1\n|other.example|11211\n"),
		"bad port":            configResponse("1\ncache.example||65536\n"),
	} {
		t.Run(name, func(t *testing.T) {
			result, err := readConfigResponse(newConfigScanner(strings.NewReader(wire)))
			require.ErrorIs(t, err, ErrMalformedResponse)
			require.Empty(t, result)
		})
	}
	result, err := readConfigResponse(newConfigScanner(strings.NewReader("END\r\n")))
	require.ErrorIs(t, err, ErrNoClusterConfig)
	require.Empty(t, result)
}

func TestReadConfigResponseLimits(t *testing.T) {
	limit := maxConfigBytes + maxConfigHeaderBytes + len("\r\nEND\r\n")
	prefix := "CONFIG cluster 0 80\n1\ncache.example|127.0.0.1|11211\n"
	for _, test := range []struct {
		name, wire string
		valid      bool
	}{
		{"at limit", prefix + strings.Repeat("\n", limit-len(prefix)-len("END\n")) + "END\n", true},
		{"terminator crosses limit", prefix + strings.Repeat("\n", limit-len(prefix)-len("END")) + "ENDinvalid\n", false},
		{"over limit", prefix + strings.Repeat("\n", limit) + "END\n", false},
		{"blank stream", strings.Repeat("\n", 2*maxConfigBytes), false},
	} {
		t.Run(test.name, func(t *testing.T) {
			input := strings.NewReader(test.wire)
			result, err := readConfigResponse(newConfigScanner(input))
			if test.valid {
				require.NoError(t, err)
				require.Equal(t, uint64(1), result.Generation)
				require.Len(t, result.Addrs, 1)
			} else {
				require.ErrorIs(t, err, ErrMalformedResponse)
				require.Empty(t, result)
				if test.name == "blank stream" {
					require.Positive(t, input.Len(), "blank lines must not allow an unbounded read")
				}
			}
		})
	}
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
		result, err = readConfigResponse(newConfigScanner(input))
		close(finished)
	}()
	_, writeErr := io.WriteString(output, configResponse("1\ncache.example|127.0.0.1|11211\n"))
	require.NoError(t, writeErr)
	waitConfigSignal(t, finished)
	require.NoError(t, err)
	require.Equal(t, uint64(1), result.Generation)
	require.Equal(t, NewAddr("tcp", "127.0.0.1:11211", 0), result.Addrs[0])
}
