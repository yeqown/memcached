package memcached

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/yeqown/memcached/resolver"

	"github.com/stretchr/testify/require"
)

func startClientDiscoveryEndpoint(t *testing.T, respond func(net.Conn) error) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	var handlers sync.WaitGroup
	var connections sync.Map
	acceptDone := make(chan struct{})
	handlers.Add(1)
	go func() {
		defer handlers.Done()
		defer close(acceptDone)
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			connections.Store(conn, struct{}{})
			handlers.Add(1)
			go func() {
				defer handlers.Done()
				defer connections.Delete(conn)
				defer func() { _ = conn.Close() }()
				command, err := bufio.NewReader(conn).ReadString('\n')
				if err != nil || command != "config get cluster\r\n" {
					t.Errorf("unexpected discovery command %q: %v", command, err)
					return
				}
				if err := respond(conn); err != nil {
					t.Errorf("discovery response: %v", err)
				}
			}()
		}
	}()
	t.Cleanup(func() {
		_ = listener.Close()
		<-acceptDone
		connections.Range(func(key, _ any) bool { _ = key.(net.Conn).Close(); return true })
		handlers.Wait()
	})
	return listener.Addr().String()
}

func clientConfigResponse(version string, nodes ...*discoveryNode) string {
	entries := make([]string, len(nodes))
	for i, node := range nodes {
		host, port, err := net.SplitHostPort(node.address)
		if err != nil {
			panic(err)
		}
		entries[i] = "|" + host + "|" + port
	}
	payload := version + "\n" + strings.Join(entries, " ") + "\n"
	return fmt.Sprintf("CONFIG cluster 0 %d\r\n%s\r\nEND\r\n", len(payload), payload)
}

func TestClientAutoDiscoveryAppliesTopologyChanges(t *testing.T) {
	a := startDiscoveryNode(t, "node-a")
	b := startDiscoveryNode(t, "node-b")
	responses := make(chan string, 4)
	requests := make(chan struct{}, 8)
	responses <- clientConfigResponse("1", a)
	target := startClientDiscoveryEndpoint(t, func(conn net.Conn) error {
		requests <- struct{}{}
		select {
		case response := <-responses:
			_, err := io.WriteString(conn, response)
			return err
		case <-t.Context().Done():
			return nil
		}
	})
	cc, err := New(target, WithResolver(resolver.NewAutoDiscovery(10*time.Millisecond)))
	require.NoError(t, err)
	c := cc.(*client)
	defer func() { require.NoError(t, c.Close()) }()
	waitRequest := func() {
		t.Helper()
		select {
		case <-requests:
		case <-time.After(2 * time.Second):
			t.Fatal("discovery request did not arrive")
		}
	}
	waitRequest()
	initial := clientView(c)
	require.Len(t, initial.addrs, 1, "initial discovery must publish its data node")
	version, err := c.Version(context.Background())
	require.NoError(t, err)
	require.Equal(t, "node-a", version)

	responses <- "SERVER_ERROR discovery unavailable\r\n"
	waitRequest()
	waitRequest() // The next retry proves the failed result has been consumed.
	require.Equal(t, initial, clientView(c))
	responses <- clientConfigResponse("2", b, a)
	require.Eventually(t, func() bool { return clientView(c).generation == 2 }, 2*time.Second, time.Millisecond)
	expanded := clientView(c)
	require.Len(t, expanded.addrs, 2)
	require.Same(t, initial.instances[resolver.AddrKey{Network: "tcp", Address: a.address}], expanded.instances[resolver.AddrKey{Network: "tcp", Address: a.address}])
	require.NoError(t, c.FlushAll(context.Background()))
	require.EqualValues(t, 1, a.flushes.Load())
	require.EqualValues(t, 1, b.flushes.Load())

	waitRequest()
	responses <- clientConfigResponse("3", b)
	require.Eventually(t, func() bool { return clientView(c).generation == 3 }, 2*time.Second, time.Millisecond)
	require.Len(t, clientView(c).addrs, 1)
	version, err = c.Version(context.Background())
	require.NoError(t, err)
	require.Equal(t, "node-b", version)
}

func TestClientCloseCancelsAutoDiscoveryRead(t *testing.T) {
	node := startDiscoveryNode(t, "node-a")
	entered := make(chan struct{})
	disconnected := make(chan struct{})
	var calls atomic.Int32
	target := startClientDiscoveryEndpoint(t, func(conn net.Conn) error {
		if calls.Add(1) == 1 {
			_, err := io.WriteString(conn, clientConfigResponse("1", node))
			return err
		}
		close(entered)
		_, err := io.Copy(io.Discard, conn)
		close(disconnected)
		return err
	})
	c, err := New(target, WithResolver(resolver.NewAutoDiscovery(10*time.Millisecond)), WithResolveTimeout(time.Minute))
	require.NoError(t, err)
	defer func() { require.NoError(t, c.Close()) }()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("background discovery did not start")
	}
	closed := make(chan error, 1)
	go func() { closed <- c.Close() }()
	select {
	case err := <-closed:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("Close did not cancel discovery")
	}
	select {
	case <-disconnected:
	case <-time.After(2 * time.Second):
		t.Fatal("Close left a discovery connection open")
	}
}

func TestClientResolverScheduleAndError(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		calls := 0
		r := resolverFunc(func(ctx context.Context, target string) (resolver.ResolveResult, *time.Time, error) {
			require.Equal(t, "http://directory", target)
			_, ok := ctx.Deadline()
			require.True(t, ok)
			calls++
			next := time.Now().Add(time.Hour)
			switch calls {
			case 1:
				return resolver.ResolveResult{}, &next, nil
			case 2:
				return resolver.ResolveResult{}, &next, errors.New("directory unavailable")
			default:
				return resolver.ResolveResult{}, nil, nil
			}
		})
		cc, err := New("http://directory", WithResolver(r))
		require.NoError(t, err)
		defer func() { require.NoError(t, cc.Close()) }()
		synctest.Wait()
		require.Equal(t, 1, calls)
		time.Sleep(time.Hour - time.Second)
		synctest.Wait()
		require.Equal(t, 1, calls, "resolution must wait for the supplied schedule")
		time.Sleep(time.Second)
		synctest.Wait()
		require.Equal(t, 2, calls)
		time.Sleep(time.Hour)
		synctest.Wait()
		require.Equal(t, 3, calls, "an error with a retry schedule must continue discovery")
		time.Sleep(24 * time.Hour)
		synctest.Wait()
		require.Equal(t, 3, calls, "a nil schedule stops discovery")
	})
}

func TestClientResolverNilErrorScheduleStopsRefresh(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		calls := 0
		r := resolverFunc(func(context.Context, string) (resolver.ResolveResult, *time.Time, error) {
			calls++
			if calls == 1 {
				next := time.Now().Add(time.Minute)
				return resolver.ResolveResult{}, &next, nil
			}
			return resolver.ResolveResult{}, nil, errors.New("stop refreshing")
		})
		cc, err := New("directory", WithResolver(r))
		require.NoError(t, err)
		defer func() { require.NoError(t, cc.Close()) }()
		time.Sleep(time.Minute)
		synctest.Wait()
		require.Equal(t, 2, calls)
		time.Sleep(time.Hour)
		synctest.Wait()
		require.Equal(t, 2, calls, "a nil schedule also stops discovery on error")
	})
}

func TestClientResolverTimeoutAndClose(t *testing.T) {
	for _, initial := range []bool{true, false} {
		t.Run(map[bool]string{true: "initial", false: "background"}[initial], func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				calls := 0
				resolveErrors := make(chan error, 1)
				r := resolverFunc(func(ctx context.Context, _ string) (resolver.ResolveResult, *time.Time, error) {
					calls++
					if initial || calls > 1 {
						<-ctx.Done()
						resolveErrors <- ctx.Err()
						return resolver.ResolveResult{}, nil, ctx.Err()
					}
					next := time.Now().Add(time.Minute)
					return resolver.ResolveResult{Addrs: []*resolver.Addr{resolver.NewAddr("tcp", "a:11211", 0)}}, &next, nil
				})
				cc, err := New("directory", WithResolver(r), WithResolveTimeout(time.Second))
				if initial {
					require.ErrorIs(t, err, context.DeadlineExceeded)
					return
				}
				require.NoError(t, err)
				time.Sleep(time.Minute)
				synctest.Wait()
				require.NoError(t, cc.Close())
				require.ErrorIs(t, <-resolveErrors, context.Canceled)
				require.NoError(t, cc.Close())
				require.Empty(t, clientView(cc.(*client)).addrs)
			})
		})
	}
}

func TestClientCloseConcurrent(t *testing.T) {
	c := topologyClient(t, []*resolver.Addr{resolver.NewAddr("tcp", "a:11211", 0)})
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() { require.NoError(t, c.Close()) })
	}
	wg.Wait()
	_, err := c.Version(context.Background())
	require.ErrorIs(t, err, ErrClientClosed)
}

func TestClientCloseConcurrentClosesAllPools(t *testing.T) {
	c := topologyClient(t, []*resolver.Addr{
		resolver.NewAddr("tcp", "a:11211", 0), resolver.NewAddr("tcp", "b:11211", 0),
	})
	initial := clientView(c)
	results := make(chan error, 20)
	for range cap(results) {
		go func() { results <- c.Close() }()
	}
	for range cap(results) {
		select {
		case err := <-results:
			require.NoError(t, err)
		case <-time.After(time.Second):
			t.Fatal("concurrent Close did not finish")
		}
	}
	for _, inst := range initial.instances {
		requirePoolClosed(t, inst.pool)
	}
	require.Empty(t, clientView(c).addrs)
}

func TestClientResolverCustomInitialTimeout(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := resolverFunc(func(ctx context.Context, _ string) (resolver.ResolveResult, *time.Time, error) {
			select {
			case <-time.After(6 * time.Second):
				return resolver.ResolveResult{Addrs: []*resolver.Addr{resolver.NewAddr("tcp", "a:11211", 0)}}, nil, nil
			case <-ctx.Done():
				return resolver.ResolveResult{}, nil, ctx.Err()
			}
		})
		c, err := New("directory", WithResolver(r), WithResolveTimeout(10*time.Second))
		require.NoError(t, err, "the configured timeout must also apply during initialization")
		require.NoError(t, c.Close())
	})
}

func TestClientResolverOwnsLifecycle(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		calls := 0
		r := resolverFunc(func(context.Context, string) (resolver.ResolveResult, *time.Time, error) {
			calls++
			if calls == 3 {
				return resolver.ResolveResult{}, nil, nil
			}
			next := time.Now().Add(time.Hour)
			if calls == 2 {
				next = time.Now().Add(30 * time.Minute)
			}
			return resolver.ResolveResult{}, &next, nil
		})
		initialCtx, cancel := context.WithCancel(t.Context())
		defer cancel()
		cc, err := NewWithContext(initialCtx, "directory", WithResolver(r))
		require.NoError(t, err)
		defer func() { require.NoError(t, cc.Close()) }()
		cancel()
		time.Sleep(time.Hour)
		synctest.Wait()
		require.Equal(t, 2, calls, "canceling the constructor context must not stop background discovery")
		time.Sleep(29 * time.Minute)
		synctest.Wait()
		require.Equal(t, 2, calls)
		time.Sleep(time.Minute)
		synctest.Wait()
		require.Equal(t, 3, calls, "unchanged topology must still use the new schedule")
	})
}

func TestClientResolverCopiesReturnedSchedule(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		a := resolver.NewAddr("tcp", "a:11211", 0)
		c := topologyClient(t, []*resolver.Addr{a})
		calls := 0
		schedule := time.Now().Add(time.Hour)
		c.options.resolver = resolverFunc(func(context.Context, string) (resolver.ResolveResult, *time.Time, error) {
			calls++
			result := resolver.ResolveResult{Generation: 1, Addrs: []*resolver.Addr{a}}
			if calls == 1 {
				return result, &schedule, nil
			}
			return result, nil, nil
		})
		next, err := c.resolve(t.Context())
		require.NoError(t, err)
		require.NotNil(t, next)
		// Mutate before starting the loop so this ownership check has no data race.
		schedule = time.Now().Add(time.Minute)
		go c.resolverLoop(c.ctx, next)
		time.Sleep(2 * time.Minute)
		synctest.Wait()
		require.Equal(t, 1, calls, "resolver-owned timestamp changes must not alter the captured schedule")
		time.Sleep(58 * time.Minute)
		synctest.Wait()
		require.Equal(t, 2, calls)
	})
}

func TestClientResolverBackgroundTimeoutAndRetry(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var calls atomic.Int32
		timeoutErrors := make(chan error, 1)
		r := resolverFunc(func(ctx context.Context, _ string) (resolver.ResolveResult, *time.Time, error) {
			call := calls.Add(1)
			next := time.Now().Add(time.Minute)
			if call == 1 {
				return resolver.ResolveResult{Generation: 1, Addrs: []*resolver.Addr{resolver.NewAddr("tcp", "a:11211", 0)}}, &next, nil
			}
			if call == 2 {
				<-ctx.Done()
				timeoutErrors <- ctx.Err()
				next = time.Now().Add(time.Minute)
				return resolver.ResolveResult{}, &next, ctx.Err()
			}
			return resolver.ResolveResult{Generation: 2, Addrs: []*resolver.Addr{resolver.NewAddr("tcp", "b:11211", 0)}}, nil, nil
		})
		cc, err := New("directory", WithResolver(r), WithResolveTimeout(time.Second))
		require.NoError(t, err)
		c := cc.(*client)
		defer func() { require.NoError(t, c.Close()) }()
		initial := clientView(c)
		time.Sleep(time.Minute)
		synctest.Wait()
		require.Equal(t, int32(2), calls.Load())
		require.Empty(t, timeoutErrors)
		time.Sleep(time.Second)
		synctest.Wait()
		require.ErrorIs(t, <-timeoutErrors, context.DeadlineExceeded)
		require.Equal(t, initial, clientView(c))
		time.Sleep(time.Minute)
		synctest.Wait()
		require.Equal(t, int32(3), calls.Load())
		view := clientView(c)
		require.Len(t, view.addrs, 1, "a successful retry must publish the recovered topology")
		require.Equal(t, "b:11211", view.addrs[0].Address)
		require.Equal(t, uint64(2), view.generation)
	})
}

func TestClientCloseRejectsLateResolveSuccess(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c := topologyClient(t, []*resolver.Addr{resolver.NewAddr("tcp", "a:11211", 0)})
		started := make(chan struct{})
		c.options.resolver = resolverFunc(func(ctx context.Context, _ string) (resolver.ResolveResult, *time.Time, error) {
			close(started)
			<-ctx.Done()
			// A result can race with cancellation even when the resolver reports success.
			return resolver.ResolveResult{Generation: 2, Addrs: []*resolver.Addr{resolver.NewAddr("tcp", "b:11211", 0)}}, nil, nil
		})
		finished := make(chan error, 1)
		go func() {
			_, err := c.resolve(c.ctx)
			finished <- err
		}()
		<-started
		require.NoError(t, c.Close())
		err := <-finished
		require.Error(t, err, "resolution interrupted by shutdown must not report success")
		require.Empty(t, clientView(c).addrs)
		require.Empty(t, c.instances, "a successful result returned after cancellation must not create pools")
	})
}

func TestClientStaticResolverRemainsUsableWithoutRefresh(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		cc, err := New("a:11211")
		require.NoError(t, err)
		c := cc.(*client)
		defer func() { require.NoError(t, c.Close()) }()
		time.Sleep(time.Hour)
		synctest.Wait()
		require.Len(t, clientView(c).addrs, 1, "static resolution must publish its node")
		inst, err := c.pickInstance(nil, nil)
		require.NoError(t, err, "finishing static resolution must not close the client")
		require.Equal(t, "a:11211", inst.addr.Address)
		inst.release()
		require.NoError(t, c.Close(), "shutdown must not wait for an unstarted resolver loop")
		_, err = c.pickInstance(nil, nil)
		require.ErrorIs(t, err, ErrClientClosed)
	})
}

func TestNewWithContextPublishesInitialTopology(t *testing.T) {
	for _, generation := range []uint64{0, 7} {
		t.Run(fmt.Sprint(generation), func(t *testing.T) {
			addr := resolver.NewAddr("tcp", "cache.example:11211", 0)
			r := resolverFunc(func(context.Context, string) (resolver.ResolveResult, *time.Time, error) {
				return resolver.ResolveResult{Generation: generation, Addrs: []*resolver.Addr{addr}}, nil, nil
			})
			cc, err := NewWithContext(t.Context(), "directory", WithResolver(r))
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, cc.Close()) })
			c := cc.(*client)
			view := clientView(c)
			require.Len(t, view.addrs, 1)
			require.Equal(t, addr.AddrKey, view.addrs[0].AddrKey)
			require.Equal(t, generation, view.generation)
			inst, err := c.pickInstance(nil, []byte("key"))
			require.NoError(t, err)
			require.Equal(t, addr.AddrKey, inst.addr.AddrKey)
			inst.release()
		})
	}
}

func TestClientResolveGeneration(t *testing.T) {
	for _, generation := range []uint64{1, 2} {
		t.Run(fmt.Sprint(generation), func(t *testing.T) {
			a := resolver.NewAddr("tcp", "a:11211", 0)
			b := resolver.NewAddr("tcp", "b:11211", 0)
			c := topologyClient(t, []*resolver.Addr{a})
			before := clientView(c)
			next := time.Now().Add(time.Hour)
			c.options.resolver = resolverFunc(func(context.Context, string) (resolver.ResolveResult, *time.Time, error) {
				return resolver.ResolveResult{Generation: generation, Addrs: []*resolver.Addr{b}}, &next, nil
			})
			gotNext, err := c.resolve(t.Context())
			require.NoError(t, err)
			require.Equal(t, next, *gotNext)
			if generation == 1 {
				require.Equal(t, before, clientView(c), "the same generation preserves the published topology")
			} else {
				view := clientView(c)
				require.Equal(t, "b:11211", view.addrs[0].Address)
				require.Equal(t, uint64(2), view.generation)
			}
		})
	}
}
