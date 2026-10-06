package resolver

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/iotest"
	"time"

	"github.com/stretchr/testify/require"
)

func configServer(t *testing.T, respond func(net.Conn) error) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	done := make(chan struct{})
	var handlers sync.WaitGroup
	go func() {
		defer close(done)
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			handlers.Add(1)
			go func() {
				defer handlers.Done()
				defer func() { _ = conn.Close() }()
				if err := conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
					t.Errorf("server deadline: %v", err)
					return
				}
				command, err := bufio.NewReader(conn).ReadString('\n')
				if err != nil || command != "config get cluster\r\n" {
					t.Errorf("unexpected discovery command %q: %v", command, err)
					return
				}
				if err := respond(conn); err != nil {
					t.Errorf("discovery server: %v", err)
				}
			}()
		}
	}()
	t.Cleanup(func() {
		require.NoError(t, listener.Close())
		<-done
		handlers.Wait()
	})
	return listener.Addr().String()
}

func TestAutoDiscoveryResolvesAndSchedulesRefresh(t *testing.T) {
	target := configServer(t, func(conn net.Conn) error {
		wire := configResponse("12\ncache.example|10.0.0.1|11211 |2001:db8::1|11212\n")
		for _, ch := range []byte(wire) {
			if _, err := conn.Write([]byte{ch}); err != nil {
				return err
			}
		}
		return nil
	})
	for _, interval := range []time.Duration{25 * time.Millisecond, 0} {
		t.Run(interval.String(), func(t *testing.T) {
			r := NewAutoDiscovery(interval)
			start := time.Now()
			result, next, err := r.Resolve(context.Background(), "tcp://"+target)
			require.NoError(t, err)
			require.Equal(t, uint64(12), result.Generation)
			require.Len(t, result.Addrs, 2)
			require.Equal(t, "10.0.0.1:11211", result.Addrs[0].Address)
			require.Equal(t, "[2001:db8::1]:11212", result.Addrs[1].Address)
			require.NotNil(t, next)
			if interval == 0 {
				interval = time.Minute
			}
			require.False(t, next.Before(start.Add(interval)))
			require.False(t, next.After(time.Now().Add(interval)))
		})
	}
}

func TestAutoDiscoveryFailureStillSchedulesRetry(t *testing.T) {
	for name, response := range map[string]string{
		"unsupported":  "ERROR\r\n",
		"unconfigured": "END\r\n",
		"truncated":    "CONFIG cluster 0 100\r\n1\ncache",
	} {
		t.Run(name, func(t *testing.T) {
			target := configServer(t, func(conn net.Conn) error {
				_, err := io.WriteString(conn, response)
				return err
			})
			interval := time.Second
			start := time.Now()
			result, next, err := NewAutoDiscovery(interval).Resolve(context.Background(), target)
			require.Error(t, err)
			require.Empty(t, result)
			require.NotNil(t, next)
			require.False(t, next.Before(start.Add(interval)))
		})
	}
}

func TestAutoDiscoveryRejectsNonTCPTarget(t *testing.T) {
	for _, target := range []string{"", "bad#host:11211", "udp://127.0.0.1:11211", "unix:///tmp/cache.sock", "host:0", "host:11211,other:11211"} {
		t.Run(target, func(t *testing.T) {
			result, next, err := NewAutoDiscovery(time.Second).Resolve(context.Background(), target)
			require.Error(t, err)
			require.Empty(t, result)
			require.NotNil(t, next)
		})
	}
}

func TestAutoDiscoveryCancellationClosesConnection(t *testing.T) {
	for _, withDeadline := range []bool{false, true} {
		t.Run(fmt.Sprint(withDeadline), func(t *testing.T) {
			entered := make(chan struct{})
			disconnected := make(chan struct{})
			target := configServer(t, func(conn net.Conn) error {
				close(entered)
				_, err := io.Copy(io.Discard, conn)
				close(disconnected)
				return err
			})
			ctx, cancel := context.WithCancel(context.Background())
			if withDeadline {
				cancel()
				ctx, cancel = context.WithTimeout(context.Background(), 100*time.Millisecond)
			}
			defer cancel()
			finished := make(chan error, 1)
			go func() {
				result, next, err := NewAutoDiscovery(time.Minute).Resolve(ctx, target)
				if len(result.Addrs) != 0 || next == nil {
					finished <- errors.New("canceled discovery lost its retry schedule or returned partial nodes")
					return
				}
				finished <- err
			}()
			select {
			case <-entered:
			case <-time.After(time.Second):
				t.Fatal("discovery command did not arrive")
			}
			wantErr := context.DeadlineExceeded
			if !withDeadline {
				cancel()
				wantErr = context.Canceled
			}
			select {
			case err := <-finished:
				require.ErrorIs(t, err, wantErr)
			case <-time.After(time.Second):
				t.Fatal("discovery ignored context cancellation")
			}
			select {
			case <-disconnected:
			case <-time.After(time.Second):
				t.Fatal("discovery left its connection open")
			}
		})
	}
}

func TestAutoDiscoveryRejectsVersionRollback(t *testing.T) {
	var calls atomic.Int32
	versions := []uint64{12, 11, 13, 13}
	target := configServer(t, func(conn net.Conn) error {
		version := versions[calls.Add(1)-1]
		_, err := io.WriteString(conn, configResponse(strconv.FormatUint(version, 10)+"\ncache.example||11211\n"))
		return err
	})
	r := NewAutoDiscovery(time.Minute)
	for _, version := range versions {
		result, next, err := r.Resolve(context.Background(), target)
		require.NotNil(t, next)
		if version == 11 {
			require.ErrorIs(t, err, ErrStaleConfig)
			require.Empty(t, result)
		} else {
			require.NoError(t, err)
			require.Equal(t, version, result.Generation)
		}
	}
}

func TestAutoDiscoveryVersionsArePerTarget(t *testing.T) {
	r := NewAutoDiscovery(time.Minute)
	for _, version := range []uint64{12, 1} {
		target := configServer(t, func(conn net.Conn) error {
			_, err := io.WriteString(conn, configResponse(strconv.FormatUint(version, 10)+"\ncache.example||11211\n"))
			return err
		})
		result, _, err := r.Resolve(context.Background(), target)
		require.NoError(t, err)
		require.Equal(t, version, result.Generation)
	}
}

func TestAutoDiscoveryConcurrentVersions(t *testing.T) {
	olderEntered := make(chan struct{})
	releaseOlder := make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(releaseOlder) }) }
	var calls atomic.Int32
	target := configServer(t, func(conn net.Conn) error {
		version := uint64(2)
		if calls.Add(1) == 1 {
			close(olderEntered)
			<-releaseOlder
			version = 1
		}
		_, err := io.WriteString(conn, configResponse(strconv.FormatUint(version, 10)+"\ncache.example||11211\n"))
		return err
	})
	t.Cleanup(release)
	r := NewAutoDiscovery(time.Minute)
	olderDone := make(chan error, 1)
	go func() {
		_, _, err := r.Resolve(context.Background(), target)
		olderDone <- err
	}()
	select {
	case <-olderEntered:
	case <-time.After(time.Second):
		t.Fatal("older discovery did not start")
	}
	result, _, err := r.Resolve(context.Background(), "tcp://"+target)
	release()
	require.NoError(t, err)
	require.Equal(t, uint64(2), result.Generation)
	select {
	case err := <-olderDone:
		require.ErrorIs(t, err, ErrStaleConfig)
	case <-time.After(time.Second):
		t.Fatal("older discovery did not finish")
	}
}

func configResponse(payload string) string {
	return fmt.Sprintf("CONFIG cluster 0 %d\r\n%s\r\nEND\r\n", len(payload), payload)
}

func TestReadConfigResponse(t *testing.T) {
	for _, test := range []struct {
		name    string
		payload string
		version uint64
		addrs   []string
	}{
		{
			name:    "AWS hostnames and missing IP",
			payload: "12\nCACHE-1.EXAMPLE.|10.0.0.1|11211 cache-2.example||011212\n",
			version: 12, addrs: []string{"10.0.0.1:11211", "cache-2.example:11212"},
		},
		{
			name:    "Google IPs and IPv6",
			payload: "2\n10.0.0.1|10.0.0.1|11211 |2001:db8::1|11212\n",
			version: 2, addrs: []string{"10.0.0.1:11211", "[2001:db8::1]:11212"},
		},
		{
			name:    "uint64 version",
			payload: "18446744073709551615\ncache.example|::ffff:127.0.0.1|11211\n",
			version: 18446744073709551615, addrs: []string{"127.0.0.1:11211"},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			// Short reads must not turn a valid config response into an error.
			result, err := readConfigResponse(iotest.OneByteReader(strings.NewReader(configResponse(test.payload))))
			require.NoError(t, err)
			require.Equal(t, test.version, result.Generation)
			require.Len(t, result.Addrs, len(test.addrs))
			for i, addr := range result.Addrs {
				require.Equal(t, "tcp", addr.Network)
				require.Equal(t, test.addrs[i], addr.Address)
			}
		})
	}
}

func TestReadConfigResponseReferenceFormat(t *testing.T) {
	for name, wire := range map[string]string{
		"reference CRLF lines": "CONFIG cluster 0 80\r\n1000\r\ncache-1.example|127.0.0.1|11211 cache-2.example|127.0.0.4|11212\n\r\nEND\r\n",
		"LF lines":             "CONFIG cluster 0 80\n1000\ncache-1.example|127.0.0.1|11211 cache-2.example|127.0.0.4|11212\nEND\n",
		"whitespace":           "\r\n  CONFIG cluster 0 80  \r\n\r\n  1000  \r\n  cache-1.example|127.0.0.1|11211   cache-2.example|127.0.0.4|11212  \r\n\r\nEND\r\n",
	} {
		t.Run(name, func(t *testing.T) {
			result, err := readConfigResponse(iotest.OneByteReader(strings.NewReader(wire)))
			require.NoError(t, err)
			require.Equal(t, uint64(1000), result.Generation)
			require.Len(t, result.Addrs, 2)
			require.Equal(t, "127.0.0.1:11211", result.Addrs[0].Address)
			require.Equal(t, "127.0.0.4:11212", result.Addrs[1].Address)
		})
	}
}

func TestReadConfigResponseStopsAtEND(t *testing.T) {
	wire := "CONFIG cluster 0 80\r\n1\r\ncache.example|127.0.0.1|11211\r\n\r\nEND\r\n"
	input, output := io.Pipe()
	t.Cleanup(func() {
		require.NoError(t, input.Close())
		require.NoError(t, output.Close())
	})
	done := make(chan error, 1)
	go func() {
		result, err := readConfigResponse(input)
		if err == nil && (result.Generation != 1 || len(result.Addrs) != 1 || result.Addrs[0].Address != "127.0.0.1:11211") {
			err = fmt.Errorf("unexpected config: %+v", result)
		}
		done <- err
	}()
	_, err := io.WriteString(output, wire)
	require.NoError(t, err)
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("parser waited for EOF after END")
	}
}

func TestReadConfigResponseBoundsStream(t *testing.T) {
	input := strings.NewReader(strings.Repeat("\n", 2*maxConfigBytes))
	result, err := readConfigResponse(input)
	require.ErrorIs(t, err, ErrMalformedResponse)
	require.Empty(t, result)
	require.Positive(t, input.Len(), "blank lines must not allow an unbounded read")
}

func TestReadConfigResponseDoesNotTruncateEND(t *testing.T) {
	limit := maxConfigBytes + maxConfigHeaderBytes + len("\r\nEND\r\n")
	prefix := "CONFIG cluster 0 80\n1\ncache.example|127.0.0.1|11211\n"
	wire := prefix + strings.Repeat("\n", limit-len(prefix)-len("END")) + "ENDinvalid\n"
	result, err := readConfigResponse(strings.NewReader(wire))
	require.ErrorIs(t, err, ErrMalformedResponse)
	require.Empty(t, result)
}

func TestReadConfigResponseAtStreamLimit(t *testing.T) {
	limit := maxConfigBytes + maxConfigHeaderBytes + len("\r\nEND\r\n")
	prefix := "CONFIG cluster 0 80\n1\ncache.example|127.0.0.1|11211\n"
	wire := prefix + strings.Repeat("\n", limit-len(prefix)-len("END\n")) + "END\n"
	result, err := readConfigResponse(strings.NewReader(wire))
	require.NoError(t, err)
	require.Equal(t, uint64(1), result.Generation)
	require.Len(t, result.Addrs, 1)
	require.Equal(t, "127.0.0.1:11211", result.Addrs[0].Address)
}

func TestReadConfigResponseRejectsInvalidFrames(t *testing.T) {
	valid := configResponse("1\ncache.example|10.0.0.1|11211\n")
	for name, wire := range map[string]string{
		"server error":         "SERVER_ERROR config unavailable\r\n",
		"unknown command":      "ERROR\r\n",
		"legacy VALUE":         "VALUE AmazonElastiCache:cluster 0 4\r\n1\na\n\r\nEND\r\n",
		"wrong key":            strings.Replace(valid, "CONFIG cluster", "CONFIG other", 1),
		"wrong flags":          strings.Replace(valid, "cluster 0", "cluster 1", 1),
		"bad length":           "CONFIG cluster 0 nope\r\n",
		"negative length":      "CONFIG cluster 0 -1\r\n",
		"empty body":           "CONFIG cluster 0 0\r\n\r\nEND\r\n",
		"oversized body":       "CONFIG cluster 0 1048577\r\n",
		"oversized header":     strings.Repeat("X", 5000) + "\r\n",
		"truncated body":       "CONFIG cluster 0 40\r\n1\ncache",
		"missing END":          strings.TrimSuffix(valid, "END\r\n"),
		"wrong terminator":     strings.TrimSuffix(valid, "END\r\n") + "OK\r\n",
		"version overflow":     configResponse("18446744073709551616\ncache.example||11211\n"),
		"negative version":     configResponse("-1\ncache.example||11211\n"),
		"nonnumeric version":   configResponse("v1\ncache.example||11211\n"),
		"empty nodes":          configResponse("1\n\n"),
		"too many lines":       configResponse("1\ncache.example||11211\nextra\n"),
		"bad node":             configResponse("1\ncache.example|11211\n"),
		"too many node fields": configResponse("1\ncache.example|10.0.0.1|11211|extra\n"),
		"empty host and IP":    configResponse("1\n||11211\n"),
		"bad hostname":         configResponse("1\nbad#host||11211\n"),
		"bad IP":               configResponse("1\ncache.example|999.999.999.999|11211\n"),
		"non-IP fallback":      configResponse("1\n|other.example|11211\n"),
		"zero port":            configResponse("1\ncache.example||0\n"),
		"large port":           configResponse("1\ncache.example||65536\n"),
		"nonnumeric port":      configResponse("1\ncache.example||abc\n"),
		"port injection":       configResponse("1\ncache.example||11211\r\nget injected\n"),
	} {
		t.Run(name, func(t *testing.T) {
			result, err := readConfigResponse(strings.NewReader(wire))
			require.ErrorIs(t, err, ErrMalformedResponse)
			require.Empty(t, result)
		})
	}
}

func TestReadConfigResponseRejectsPaddedHeader(t *testing.T) {
	wire := strings.Repeat(" ", maxConfigHeaderBytes+1) + configResponse("1\ncache.example|127.0.0.1|11211\n")
	result, err := readConfigResponse(strings.NewReader(wire))
	require.ErrorIs(t, err, ErrMalformedResponse)
	require.Empty(t, result)
}

func TestReadConfigResponseNoConfig(t *testing.T) {
	result, err := readConfigResponse(strings.NewReader("END\r\n"))
	require.ErrorIs(t, err, ErrNoClusterConfig)
	require.Empty(t, result)
}
