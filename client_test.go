package memcached

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	pkgerrors "github.com/pkg/errors"
	"github.com/stretchr/testify/require"
	memcodec "github.com/yeqown/memcached/codec"
	"github.com/yeqown/memcached/internal/testutil"
	"github.com/yeqown/memcached/picker"
	"github.com/yeqown/memcached/resolver"
	"github.com/yeqown/memcached/telemetry"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

func TestClientCloseConcurrent(t *testing.T) {
	c := newTestClient(t, []*resolver.Addr{
		resolver.NewAddr("tcp", "a:11211", 0), resolver.NewAddr("tcp", "b:11211", 0),
	})
	initial := clientView(c)
	var conns []*testConn
	for _, n := range initial.nodes {
		raw := newTestConn()
		conns = append(conns, raw)
		n.pool.createConn = func(context.Context) (memcachedConn, error) { return raw, nil }
		cn, err := n.pool.get(t.Context())
		require.NoError(t, err)
		require.NoError(t, n.pool.put(cn))
	}
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
	for _, inst := range initial.nodes {
		requirePoolClosed(t, inst.pool)
	}
	for _, cn := range conns {
		require.EqualValues(t, 1, cn.closes.Load(), "concurrent close must clean up each connection once")
	}
	_, closedErr := c.topology.allNodes()
	require.ErrorIs(t, closedErr, ErrClientClosed)
	_, err := c.Version(t.Context())
	require.ErrorIs(t, err, ErrClientClosed)
}

func TestClientCloseAggregatesNodeErrors(t *testing.T) {
	firstErr, secondErr := errors.New("first node close failed"), errors.New("second node close failed")
	cc, err := New("a:11211,b:11211", WithMaxConns(1), WithMaxIdleConns(1))
	require.NoError(t, err)
	// The close errors are expected and checked below; cleanup must still run if setup fails.
	t.Cleanup(func() { _ = cc.Close() })
	c := cc.(*client)
	view := clientView(c)
	raws := make([]*testConn, 0, 2)
	for _, test := range []struct {
		address string
		err     error
	}{
		{address: "a:11211", err: firstErr},
		{address: "b:11211", err: secondErr},
	} {
		raw := newTestConn()
		raw.closeErr = test.err
		raws = append(raws, raw)
		n := view.nodes[resolver.AddrKey{Network: "tcp", Address: test.address}]
		n.pool.createConn = func(context.Context) (memcachedConn, error) { return raw, nil }
		cn, getErr := n.pool.get(t.Context())
		require.NoError(t, getErr)
		require.NoError(t, n.pool.put(cn))
	}
	closeErr := cc.Close()
	require.ErrorIs(t, closeErr, firstErr)
	require.ErrorIs(t, closeErr, secondErr)
	require.NoError(t, cc.Close(), "repeated close must not retry node cleanup")
	for _, raw := range raws {
		require.EqualValues(t, 1, raw.closes.Load())
	}
}

func TestClientRejectsInvalidInitialTopology(t *testing.T) {
	for _, test := range []struct {
		name  string
		addrs []*resolver.Addr
	}{
		{name: "empty"},
		{name: "nil node", addrs: []*resolver.Addr{nil}},
		{name: "conflicting identity", addrs: []*resolver.Addr{resolver.NewAddr("tcp", "a:11211", 1), resolver.NewAddr("tcp", "a:11211", 2)}},
	} {
		t.Run(test.name, func(t *testing.T) {
			cc, err := New("directory", WithResolver(resolverFunc(func(context.Context, string) (resolver.ResolveResult, *time.Time, error) {
				return resolver.ResolveResult{Addrs: test.addrs}, nil, nil
			})))
			if cc != nil {
				require.NoError(t, cc.Close())
			}
			require.ErrorIs(t, err, ErrInvalidAddress)
			require.Nil(t, cc)
		})
	}
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
		var calls atomic.Int32
		r := resolverFunc(func(context.Context, string) (resolver.ResolveResult, *time.Time, error) {
			call := calls.Add(1)
			if call == 3 {
				return resolver.ResolveResult{Addrs: []*resolver.Addr{resolver.NewAddr("tcp", "a:11211", 0)}}, nil, nil
			}
			next := time.Now().Add(time.Hour)
			if call == 2 {
				next = time.Now().Add(30 * time.Minute)
			}
			return resolver.ResolveResult{Addrs: []*resolver.Addr{resolver.NewAddr("tcp", "a:11211", 0)}}, &next, nil
		})
		initialCtx, cancel := context.WithCancel(t.Context())
		defer cancel()
		cc, err := NewWithContext(initialCtx, "directory", WithResolver(r))
		require.NoError(t, err)
		defer func() { require.NoError(t, cc.Close()) }()
		cancel()
		time.Sleep(time.Hour)
		synctest.Wait()
		require.Equal(t, 2, int(calls.Load()), "canceling the constructor context must not stop background discovery")
		time.Sleep(29 * time.Minute)
		synctest.Wait()
		require.Equal(t, 2, int(calls.Load()))
		time.Sleep(time.Minute)
		synctest.Wait()
		require.Equal(t, 3, int(calls.Load()), "unchanged topology must still use the new schedule")
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
		inst, err := c.topology.pickNode(c.options.picker, nil, nil)
		require.NoError(t, err, "finishing static resolution must not close the client")
		require.Equal(t, "a:11211", inst.addr.Address)
		require.NoError(t, c.Close(), "shutdown must not wait for an unstarted resolver loop")
		_, err = c.topology.pickNode(c.options.picker, nil, nil)
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
			inst, err := c.topology.pickNode(c.options.picker, nil, []byte("key"))
			require.NoError(t, err)
			require.Equal(t, addr.AddrKey, inst.addr.AddrKey)
		})
	}
}

func TestCompressionDisablesAppendPrepend(t *testing.T) {
	c := &client{options: newClientOptions()}
	c.options.codec = mustCompressCodec(t, memcodec.CompressionAlgorithmDeflate, 0, 6)
	for _, operation := range []string{"append", "prepend", "meta append", "meta prepend"} {
		t.Run(operation, func(t *testing.T) {
			var err error
			switch operation {
			case "append":
				err = c.Append(t.Context(), "key", []byte("value"), 0, 0)
			case "prepend":
				err = c.Prepend(t.Context(), "key", []byte("value"), 0, 0)
			default:
				mode := MetaSetModeAppend
				if operation == "meta prepend" {
					mode = MetaSetModePrepend
				}
				var item *MetaItem
				item, err = c.MetaSet(t.Context(), []byte("key"), []byte("value"), MetaSetFlagModeSwitch(mode))
				require.Nil(t, item)
			}
			require.ErrorIs(t, err, ErrNotSupported)
		})
	}
}

func TestClientPickConnReleaseDrainsNodeOnce(t *testing.T) {
	a := resolver.NewAddr("tcp", "a:11211", 0)
	c := newTestClient(t, []*resolver.Addr{a})
	n := clientView(c).nodes[a.AddrKey]
	raw := newTestConn()
	n.pool.createConn = func(context.Context) (memcachedConn, error) { return raw, nil }
	_, cn, releaseFn, err := c.pickConn(t.Context(), nil, nil)
	require.NoError(t, err)
	defer releaseFn(nil)
	require.Same(t, raw, cn)
	require.NoError(t, updateTopology(c, []*resolver.Addr{resolver.NewAddr("tcp", "b:11211", 0)}))
	require.NoError(t, c.Close())
	require.Zero(t, raw.closes.Load(), "the lease preserves the borrowed connection during shutdown")
	releaseFn(nil)
	releaseFn(nil)
	requirePoolClosed(t, n.pool)
	require.EqualValues(t, 1, raw.closes.Load())
	require.Equal(t, nodeClosed, n.status())
}

func TestClientPickConnFailureDoesNotLeakRemovedNode(t *testing.T) {
	for _, stage := range []string{"dial failure", "interrupted pool wait"} {
		t.Run(stage, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				a := resolver.NewAddr("tcp", "a:11211", 0)
				c := newTestClient(t, []*resolver.Addr{a}, WithMaxConns(1))
				n := clientView(c).nodes[a.AddrKey]
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				failure := pkgerrors.New("dial failed")
				proceed := make(chan struct{})
				var releaseOwner func(error)
				if stage == "interrupted pool wait" {
					n.pool.createConn = func(context.Context) (memcachedConn, error) {
						return newTestConn(), nil
					}
					var err error
					_, _, releaseOwner, err = c.pickConn(ctx, nil, nil)
					require.NoError(t, err)
					defer releaseOwner(nil)
					failure = nil
				} else {
					n.pool.createConn = func(context.Context) (memcachedConn, error) {
						<-proceed
						return nil, failure
					}
				}
				done := make(chan poolGetResult, 1)
				go func() {
					_, cn, releaseFn, err := c.pickConn(ctx, nil, nil)
					if releaseFn != nil {
						releaseFn(err)
					}
					done <- poolGetResult{cn, err}
				}()
				synctest.Wait()
				require.Empty(t, done)
				require.NoError(t, updateTopology(c, []*resolver.Addr{resolver.NewAddr("tcp", "b:11211", 0)}))
				if releaseOwner == nil {
					close(proceed)
				}
				got := <-done
				require.Nil(t, got.cn)
				if failure != nil {
					require.ErrorIs(t, got.err, failure)
				} else {
					require.ErrorContains(t, got.err, "connection pool is closed")
				}
				if releaseOwner != nil {
					releaseOwner(nil)
				}
				requirePoolClosed(t, n.pool)
			})
		})
	}
}

func TestClientPickConnTracesBorrowingAndRequestResult(t *testing.T) {
	for _, stage := range []string{"success", "borrow failure", "request failure"} {
		t.Run(stage, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				a := resolver.NewAddr("tcp", "a:11211", 0)
				c := newTestClient(t, []*resolver.Addr{a})
				span := &requestTraceSpan{Span: trace.SpanFromContext(t.Context())}
				provider := &requestTraceProvider{
					TracerProvider: noop.NewTracerProvider(),
					tracer:         &requestTracer{Tracer: noop.NewTracerProvider().Tracer("test"), span: span},
				}
				c.tracer = telemetry.NewConfig(telemetry.WithTracerProvider(provider)).Tracer()
				failure := pkgerrors.New("request failed")
				clientView(c).nodes[a.AddrKey].pool.createConn = func(ctx context.Context) (memcachedConn, error) {
					require.Same(t, span, trace.SpanFromContext(ctx), "borrowing must use the request span context")
					time.Sleep(100 * time.Millisecond)
					if stage == "borrow failure" {
						return nil, failure
					}
					return newTestConn(), nil
				}
				ctx, _, releaseFn, err := c.pickConn(t.Context(), []byte("get"), []byte("key"))
				if stage == "borrow failure" {
					require.ErrorIs(t, err, failure)
					require.Nil(t, releaseFn)
					require.Equal(t, 100*time.Millisecond, span.duration)
				} else {
					require.NoError(t, err)
					defer releaseFn(nil)
					require.Same(t, span, trace.SpanFromContext(ctx))
					require.Zero(t, span.ends, "a borrowed connection keeps its span open")
					time.Sleep(200 * time.Millisecond)
					if stage == "request failure" {
						releaseFn(failure)
					} else {
						releaseFn(nil)
					}
					releaseFn(nil)
					require.Equal(t, 300*time.Millisecond, span.duration, "duration includes borrowing and the request")
				}
				require.Equal(t, 1, span.ends)
				if stage == "success" {
					require.Nil(t, span.err)
					require.Equal(t, codes.Ok, span.status)
				} else {
					require.ErrorIs(t, span.err, failure)
					require.Equal(t, codes.Error, span.status)
				}
			})
		})
	}
}

type requestTraceProvider struct {
	trace.TracerProvider
	tracer trace.Tracer
}

func (p *requestTraceProvider) Tracer(string, ...trace.TracerOption) trace.Tracer {
	return p.tracer
}

type requestTracer struct {
	trace.Tracer
	span *requestTraceSpan
}

func (t *requestTracer) Start(ctx context.Context, _ string, _ ...trace.SpanStartOption) (context.Context, trace.Span) {
	t.span.start = time.Now()
	return trace.ContextWithSpan(ctx, t.span), t.span
}

type requestTraceSpan struct {
	trace.Span
	start    time.Time
	duration time.Duration
	ends     int
	status   codes.Code
	err      error
}

func (s *requestTraceSpan) End(...trace.SpanEndOption) {
	s.ends++
	s.duration = time.Since(s.start)
}

func (s *requestTraceSpan) SetStatus(code codes.Code, _ string) { s.status = code }

func (s *requestTraceSpan) RecordError(err error, _ ...trace.EventOption) {
	s.err = err
}

func TestClientDispatchRecordsIOErrors(t *testing.T) {
	for _, stage := range []string{"send", "receive"} {
		t.Run(stage, func(t *testing.T) {
			failure := errors.New("transport failed")
			raw := newTestConn()
			if stage == "send" {
				raw.writeErr = failure
			} else {
				raw.readErr = failure
			}
			c := clientWithConn(t, raw)
			span := &requestTraceSpan{Span: trace.SpanFromContext(t.Context())}
			provider := &requestTraceProvider{
				TracerProvider: noop.NewTracerProvider(),
				tracer:         &requestTracer{Tracer: noop.NewTracerProvider().Tracer("test"), span: span},
			}
			c.tracer = telemetry.NewConfig(telemetry.WithTracerProvider(provider)).Tracer()
			_, err := c.Version(t.Context())
			require.ErrorIs(t, err, failure)
			require.ErrorIs(t, span.err, failure)
			require.Equal(t, codes.Error, span.status)
			require.Equal(t, 1, span.ends)
			require.Equal(t, 1, raw.pool.stats().IdleConns)
		})
	}
}

func TestClientRequestsKeepSelectedNode(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		a, b := resolver.NewAddr("tcp", "a:11211", 0), resolver.NewAddr("tcp", "b:11211", 0)
		started, proceed := make(chan struct{}), make(chan struct{})
		finish := sync.OnceFunc(func() { close(proceed) })
		defer finish()
		rawA := newTestConn()
		rawA.respond = func([]byte) ([]byte, error) {
			close(started)
			<-proceed
			return []byte("VERSION a\r\n"), nil
		}
		c := newTestClient(t, []*resolver.Addr{a})
		old := clientView(c).nodes[a.AddrKey]
		old.pool.createConn = func(context.Context) (memcachedConn, error) { return rawA, nil }
		type result struct {
			version string
			err     error
		}
		done := make(chan result, 1)
		go func() { v, err := c.Version(t.Context()); done <- result{v, err} }()
		<-started
		require.NoError(t, updateTopology(c, []*resolver.Addr{b}))
		require.Zero(t, rawA.closes.Load(), "the selected request keeps its lease")
		rawB := newTestConn([]byte("VERSION b\r\n"))
		clientView(c).nodes[b.AddrKey].pool.createConn = func(context.Context) (memcachedConn, error) { return rawB, nil }
		v, err := c.Version(t.Context())
		require.NoError(t, err)
		require.Equal(t, "b", v)
		finish()
		got := <-done
		require.NoError(t, got.err)
		require.Equal(t, "a", got.version)
		require.EqualValues(t, 1, rawA.closes.Load())
		requirePoolClosed(t, old.pool)
	})
}

func TestClientRejectsPickerOutsideActiveAddresses(t *testing.T) {
	for name, result := range map[string]*resolver.Addr{"nil": nil, "outside": resolver.NewAddr("tcp", "b:11211", 0)} {
		t.Run(name, func(t *testing.T) {
			picker := testPickerFunc(func([]*resolver.Addr, []byte, []byte) (*resolver.Addr, error) { return result, nil })
			c := newTestClient(t, []*resolver.Addr{resolver.NewAddr("tcp", "a:11211", 0)}, WithPicker(picker))
			_, err := c.Version(context.Background())
			require.ErrorIs(t, err, ErrInvalidAddress)
			for _, p := range clientView(c).nodes {
				require.Zero(t, p.pool.stats().TotalConns)
			}
		})
	}
}

func TestClientBroadcastRejectsRemovedUnborrowedTarget(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		a, b := resolver.NewAddr("tcp", "a:11211", 0), resolver.NewAddr("tcp", "b:11211", 0)
		c := newTestClient(t, []*resolver.Addr{a, b})
		initial := clientView(c)
		first := initial.nodes[resolver.AddrKey{Network: "tcp", Address: "a:11211"}]
		second := initial.nodes[resolver.AddrKey{Network: "tcp", Address: "b:11211"}]
		started, proceed := make(chan struct{}), make(chan struct{})
		finish := sync.OnceFunc(func() { close(proceed) })
		defer finish()
		rawA := newTestConn()
		rawB := newTestConn()
		first.pool.createConn = func(ctx context.Context) (memcachedConn, error) {
			close(started)
			select {
			case <-proceed:
				return rawA, nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		second.pool.createConn = func(context.Context) (memcachedConn, error) { return rawB, nil }
		called := make(chan *connPool, 2)
		done := make(chan error, 1)
		go func() {
			done <- c.broadcastRequest(context.Background(), func(_ context.Context, cn memcachedConn) error {
				called <- cn.getConnPool()
				return nil
			})
		}()
		<-started
		synctest.Wait()
		require.Same(t, second.pool, <-called)
		require.NoError(t, updateTopology(c, []*resolver.Addr{b, resolver.NewAddr("tcp", "c:11211", 0)}))
		finish()
		require.Error(t, <-done, "removal rejects a target that is still dialing")
		require.Empty(t, called, "removed and newly added targets must not execute")
		require.EqualValues(t, 1, rawA.closes.Load())
		requirePoolClosed(t, first.pool)
		require.Zero(t, rawB.closes.Load(), "the retained node keeps its idle connection")
		require.Zero(t, clientView(c).nodes[resolver.AddrKey{Network: "tcp", Address: "c:11211"}].pool.stats().TotalConns)
	})
}

func TestClientBroadcastKeepsOriginalTargets(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		a, b, d := resolver.NewAddr("tcp", "a:11211", 0), resolver.NewAddr("tcp", "b:11211", 0), resolver.NewAddr("tcp", "d:11211", 0)
		c := newTestClient(t, []*resolver.Addr{a, b})
		initial := clientView(c)
		rawA, rawB, rawD := newTestConn([]byte("OK\r\n")), newTestConn([]byte("OK\r\n")), newTestConn([]byte("OK\r\n"))
		initial.nodes[a.AddrKey].pool.createConn = func(context.Context) (memcachedConn, error) { return rawA, nil }
		initial.nodes[b.AddrKey].pool.createConn = func(context.Context) (memcachedConn, error) { return rawB, nil }
		gate := make(chan struct{})
		finish := sync.OnceFunc(func() { close(gate) })
		defer finish()
		entered := make(chan *connPool, 2)
		done := make(chan error, 1)
		go func() {
			done <- c.broadcastRequest(t.Context(), func(_ context.Context, cn memcachedConn) error {
				entered <- cn.getConnPool()
				<-gate
				if _, err := cn.Write([]byte("flush_all\r\n")); err != nil {
					return err
				}
				_, err := cn.readLine('\n')
				return err
			})
		}()
		targets := []*connPool{<-entered, <-entered}
		require.ElementsMatch(t, []*connPool{initial.nodes[a.AddrKey].pool, initial.nodes[b.AddrKey].pool}, targets)
		require.NoError(t, updateTopology(c, []*resolver.Addr{b, d}))
		clientView(c).nodes[d.AddrKey].pool.createConn = func(context.Context) (memcachedConn, error) { return rawD, nil }
		finish()
		require.NoError(t, <-done)
		require.Equal(t, [][]byte{[]byte("flush_all\r\n")}, rawA.writes)
		require.Equal(t, rawA.writes, rawB.writes)
		require.Empty(t, rawD.writes, "new members wait for the next broadcast")
		requirePoolClosed(t, initial.nodes[a.AddrKey].pool)
		require.NoError(t, c.FlushAll(t.Context()))
		require.Len(t, rawA.writes, 1)
		require.Len(t, rawB.writes, 2)
		require.Equal(t, rawA.writes, rawD.writes)
	})
}

func TestClientBroadcastContinuesAfterFailure(t *testing.T) {
	for _, stage := range []string{"borrow", "call"} {
		t.Run(stage, func(t *testing.T) {
			a, b := resolver.NewAddr("tcp", "a:11211", 0), resolver.NewAddr("tcp", "b:11211", 0)
			c := newTestClient(t, []*resolver.Addr{a, b})
			view := clientView(c)
			first, second := view.nodes[a.AddrKey], view.nodes[b.AddrKey]
			failure := errors.New("node failed")
			first.pool.createConn = func(context.Context) (memcachedConn, error) {
				if stage == "borrow" {
					return nil, failure
				}
				return newTestConn(), nil
			}
			second.pool.createConn = func(context.Context) (memcachedConn, error) {
				return newTestConn(), nil
			}
			called := make(chan *connPool, 2)
			err := c.broadcastRequest(t.Context(), func(_ context.Context, cn memcachedConn) error {
				if cn.getConnPool() == first.pool {
					return failure
				}
				called <- cn.getConnPool()
				return nil
			})
			require.ErrorIs(t, err, failure)
			require.Len(t, called, 1, "the healthy target must run even when another target fails")
			require.Same(t, second.pool, <-called)
			require.Equal(t, 1, second.pool.stats().IdleConns)
			require.NoError(t, c.Close())
			requirePoolClosed(t, first.pool)
			requirePoolClosed(t, second.pool)
		})
	}
}

func startClientDiscoveryEndpoint(t *testing.T, respond func(net.Conn) error) string {
	t.Helper()
	return startTCPTestServer(t, func(conn net.Conn) {
		reader := bufio.NewReader(conn)
		for {
			command, err := reader.ReadString('\n')
			if err != nil {
				return
			}
			if command != "config get cluster\r\n" {
				t.Errorf("unexpected discovery command %q", command)
				return
			}
			if err := respond(conn); err != nil {
				t.Errorf("discovery response: %v", err)
				return
			}
		}
	})
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
	require.Same(t, initial.nodes[resolver.AddrKey{Network: "tcp", Address: a.address}], expanded.nodes[resolver.AddrKey{Network: "tcp", Address: a.address}])
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

type discoveryNode struct {
	address string
	flushes atomic.Int32
}

func startDiscoveryNode(t *testing.T, version string) *discoveryNode {
	t.Helper()
	n := &discoveryNode{}
	n.address = startTCPTestServer(t, func(cn net.Conn) {
		r := bufio.NewReader(cn)
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			switch line {
			case "version\r\n":
				_, _ = fmt.Fprintf(cn, "VERSION %s\r\n", version)
			case "flush_all\r\n":
				n.flushes.Add(1)
				_, _ = io.WriteString(cn, "OK\r\n")
			default:
				return
			}
		}
	})
	return n
}

func TestClientDiscoveryConcurrentRequestsBroadcastAndClose(t *testing.T) {
	a, b := startDiscoveryNode(t, "a"), startDiscoveryNode(t, "b")
	var calls atomic.Int32
	refreshed := make(chan struct{})
	refreshGate := make(chan struct{})
	allowRefresh := sync.OnceFunc(func() { close(refreshGate) })
	defer allowRefresh()
	r := resolverFunc(func(ctx context.Context, _ string) (resolver.ResolveResult, *time.Time, error) {
		if err := ctx.Err(); err != nil {
			return resolver.ResolveResult{}, nil, err
		}
		n := calls.Add(1)
		if n > 1 {
			select {
			case <-refreshGate:
			case <-ctx.Done():
				return resolver.ResolveResult{}, nil, ctx.Err()
			}
		}
		if n == 10 {
			close(refreshed)
		}
		addrs := []*resolver.Addr{resolver.NewAddr("tcp", a.address, 0)}
		if n%2 != 0 {
			addrs = append(addrs, resolver.NewAddr("tcp", b.address, 0))
		}
		next := time.Now().Add(time.Millisecond)
		return resolver.ResolveResult{Generation: uint64(n), Addrs: addrs}, &next, nil
	})
	cc, err := New("directory", WithResolver(r), WithPicker(picker.NewRendezvousHashPicker(42)), WithMaxConns(4), WithMaxIdleConns(2))
	require.NoError(t, err)
	c := cc.(*client)
	defer func() { require.NoError(t, c.Close()) }()
	initial := clientView(c)
	require.Len(t, initial.addrs, 2, "concurrent requests require the initial topology to be published")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var completed atomic.Int32
	var wg sync.WaitGroup
	errCh := make(chan error, 8)
	ready := make(chan struct{}, 8)
	for i := range 8 {
		wg.Go(func() {
			ready <- struct{}{}
			for {
				var err error
				if i%2 == 0 {
					var version string
					version, err = c.Version(ctx)
					if err == nil && version != "a" && version != "b" {
						err = fmt.Errorf("unexpected node response %q", version)
					}
				} else {
					err = c.FlushAll(ctx)
				}
				if errors.Is(err, ErrClientClosed) {
					return
				}
				if errors.Is(err, ErrInstanceAbnormal) || err != nil && strings.Contains(err.Error(), "connection pool is closed") {
					continue
				}
				if err != nil {
					errCh <- err
					return
				}
				completed.Add(1)
			}
		})
	}
	for range cap(ready) {
		select {
		case <-ready:
		case <-ctx.Done():
			t.Fatal("request workers did not start")
		}
	}
	allowRefresh()
	select {
	case <-refreshed:
	case <-ctx.Done():
		t.Fatal("discovery did not refresh while requests were running")
	}
	require.Eventually(t, func() bool { return clientView(c).generation >= 10 }, time.Second, time.Millisecond,
		"discovery must publish changed topologies while requests run")
	require.NoError(t, c.Close())
	wg.Wait()
	close(errCh)
	for err := range errCh {
		require.NoError(t, err)
	}
	require.Positive(t, completed.Load())
	_, closedErr := c.topology.allNodes()
	require.ErrorIs(t, closedErr, ErrClientClosed)
	for _, p := range initial.nodes {
		requirePoolClosed(t, p.pool)
	}
}

func TestClientRequestCancellationCancelsSASLHandshake(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer func() { _ = l.Close() }()
	started := make(chan struct{})
	serverDone := make(chan struct{})
	go func() {
		defer close(serverDone)
		cn, err := l.Accept()
		if err != nil {
			return
		}
		defer func() { _ = cn.Close() }()
		request := make([]byte, 24)
		if _, err := io.ReadFull(cn, request); err != nil {
			return
		}
		close(started)
		_, _ = io.Copy(io.Discard, cn) // Stall authentication until the request is canceled.
	}()
	cc, err := New(l.Addr().String(), WithSASL("user", "password"))
	require.NoError(t, err)
	c := cc.(*client)
	t.Cleanup(func() { require.NoError(t, c.Close()) })
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := c.Version(ctx); done <- err }()
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal("SASL handshake did not start")
	}
	cancel()
	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("request cancellation did not cancel authentication")
	}
	<-serverDone
}

func TestClientStorageCommands(t *testing.T) {
	for _, test := range []struct {
		name string
		call func(*client) error
		wire string
	}{
		{"set", func(c *client) error { return c.Set(t.Context(), "key", []byte("value"), 7, time.Minute) }, "set key 7 60 5\r\nvalue\r\n"},
		{"add", func(c *client) error { return c.Add(t.Context(), "key", []byte("value"), 7, time.Minute) }, "add key 7 60 5\r\nvalue\r\n"},
		{"replace", func(c *client) error { return c.Replace(t.Context(), "key", []byte("value"), 7, time.Minute) }, "replace key 7 60 5\r\nvalue\r\n"},
		{"append", func(c *client) error { return c.Append(t.Context(), "key", []byte("value"), 7, time.Minute) }, "append key 7 60 5\r\nvalue\r\n"},
		{"prepend", func(c *client) error { return c.Prepend(t.Context(), "key", []byte("value"), 7, time.Minute) }, "prepend key 7 60 5\r\nvalue\r\n"},
		{"cas", func(c *client) error { return c.Cas(t.Context(), "key", []byte("value"), 7, time.Minute, 42) }, "cas key 7 60 5 42\r\nvalue\r\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			raw := newTestConn([]byte("STORED\r\n"))
			require.NoError(t, test.call(clientWithConn(t, raw)))
			require.Equal(t, [][]byte{[]byte(test.wire)}, raw.writes)
		})
	}
}

func TestClientStatusCommands(t *testing.T) {
	for _, test := range []struct {
		name           string
		call           func(*client) error
		wire, response string
	}{
		{"delete", func(c *client) error { return c.Delete(t.Context(), "key") }, "delete key\r\n", "DELETED\r\n"},
		{"touch", func(c *client) error { return c.Touch(t.Context(), "key", time.Minute) }, "touch key 60\r\n", "TOUCHED\r\n"},
		{"flush", func(c *client) error { return c.FlushAll(t.Context()) }, "flush_all\r\n", "OK\r\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			raw := newTestConn([]byte(test.response))
			require.NoError(t, test.call(clientWithConn(t, raw)))
			require.Equal(t, [][]byte{[]byte(test.wire)}, raw.writes)
		})
	}
}

func TestClientArithmeticCommands(t *testing.T) {
	for _, test := range []struct {
		name string
		call func(*client) (uint64, error)
	}{
		{"incr", func(c *client) (uint64, error) { return c.Incr(t.Context(), "key", 3) }},
		{"decr", func(c *client) (uint64, error) { return c.Decr(t.Context(), "key", 3) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			raw := newTestConn([]byte("12\r\n"))
			value, err := test.call(clientWithConn(t, raw))
			require.NoError(t, err)
			require.Equal(t, uint64(12), value)
			require.Equal(t, [][]byte{[]byte(test.name + " key 3\r\n")}, raw.writes)
		})
	}
}

func TestClientRetrievalRoutesByFirstKey(t *testing.T) {
	for _, test := range []struct {
		cmd, wire, response string
		call                func(*client) ([]*Item, error)
	}{
		{"get", "get first\r\n", "VALUE first 7 5\r\nvalue\r\nEND\r\n", func(c *client) ([]*Item, error) { item, err := c.Get(t.Context(), "first"); return []*Item{item}, err }},
		{"gets", "gets first second\r\n", "VALUE first 7 5 42\r\nvalue\r\nVALUE second 7 5 43\r\nvalue\r\nEND\r\n", func(c *client) ([]*Item, error) { return c.Gets(t.Context(), "first", "second") }},
		{"gat", "gat 60 first\r\n", "VALUE first 7 5\r\nvalue\r\nEND\r\n", func(c *client) ([]*Item, error) {
			item, err := c.GetAndTouch(t.Context(), time.Minute, "first")
			return []*Item{item}, err
		}},
		{"gats", "gats 60 first second\r\n", "VALUE first 7 5 42\r\nvalue\r\nVALUE second 7 5 43\r\nvalue\r\nEND\r\n", func(c *client) ([]*Item, error) { return c.GetAndTouches(t.Context(), time.Minute, "first", "second") }},
	} {
		t.Run(test.cmd, func(t *testing.T) {
			var cmd, key []byte
			p := testPickerFunc(func(addrs []*resolver.Addr, pickedCmd, pickedKey []byte) (*resolver.Addr, error) {
				cmd, key = append([]byte(nil), pickedCmd...), append([]byte(nil), pickedKey...)
				return addrs[len(addrs)-1], nil
			})
			a, b := resolver.NewAddr("tcp", "a:11211", 0), resolver.NewAddr("tcp", "b:11211", 0)
			c := newTestClient(t, []*resolver.Addr{a, b}, WithPicker(p))
			raw := newTestConn([]byte(test.response))
			clientView(c).nodes[a.AddrKey].pool.createConn = func(context.Context) (memcachedConn, error) { return nil, fmt.Errorf("routed to wrong node") }
			clientView(c).nodes[b.AddrKey].pool.createConn = func(context.Context) (memcachedConn, error) { return raw, nil }
			items, err := test.call(c)
			require.NoError(t, err)
			wantCount := 1
			if test.cmd == "gets" || test.cmd == "gats" {
				wantCount = 2
			}
			require.Len(t, items, wantCount)
			for i, item := range items {
				require.NotNil(t, item)
				require.Equal(t, []byte("value"), item.Value)
				require.Equal(t, uint32(7), item.Flags)
				if wantCount == 2 {
					require.Equal(t, uint64(42+i), item.CAS)
				}
			}
			require.Equal(t, test.cmd, string(cmd))
			require.Equal(t, "first", string(key))
			require.Equal(t, [][]byte{[]byte(test.wire)}, raw.writes)
		})
	}
}

func TestClientRetrievalMissingAndEmptyKeys(t *testing.T) {
	raw := newTestConn([]byte("END\r\n"))
	c := clientWithConn(t, raw)
	item, err := c.Get(t.Context(), "missing")
	require.ErrorIs(t, err, ErrNotFound)
	require.Nil(t, item)
	items, err := c.Gets(t.Context())
	require.NoError(t, err)
	require.Empty(t, items)
	items, err = c.GetAndTouches(t.Context(), time.Minute)
	require.NoError(t, err)
	require.Empty(t, items)
	require.Len(t, raw.writes, 1, "empty key lists do not send a request")
}

func TestClientMetaCommands(t *testing.T) {
	for _, test := range []struct {
		name, wire, response string
		call                 func(*client) (*MetaItem, error)
		want                 MetaItem
	}{
		{"set", "ms key 5 c F7 T60\r\nvalue\r\n", "HD c42\r\n", func(c *client) (*MetaItem, error) {
			return c.MetaSet(t.Context(), []byte("key"), []byte("value"), MetaSetFlagClientFlags(7), MetaSetFlagTTL(60), MetaSetFlagReturnCAS())
		}, MetaItem{Key: []byte("key"), TTL: 60, Flags: 7, CAS: 42}},
		{"get", "mg key c f t v\r\n", "VA 5 c42 f7 t60\r\nvalue\r\n", func(c *client) (*MetaItem, error) {
			return c.MetaGet(t.Context(), []byte("key"), MetaGetFlagReturnValue(), MetaGetFlagReturnTTL(), MetaGetFlagReturnCAS())
		}, MetaItem{Key: []byte("key"), Value: []byte("value"), TTL: 60, Flags: 7, CAS: 42, Size: 5}},
		{"delete", "md key\r\n", "HD\r\n", func(c *client) (*MetaItem, error) { return c.MetaDelete(t.Context(), []byte("key")) }, MetaItem{Key: []byte("key")}},
		{"arithmetic", "ma key D3 c v\r\n", "VA 2 c42\r\n12\r\n", func(c *client) (*MetaItem, error) {
			return c.MetaArithmetic(t.Context(), []byte("key"), 3, MetaArithmeticFlagReturnValue(), MetaArithmeticFlagReturnCAS())
		}, MetaItem{Key: []byte("key"), Value: []byte("12"), CAS: 42, Size: 2}},
	} {
		t.Run(test.name, func(t *testing.T) {
			raw := newTestConn([]byte(test.response))
			item, err := test.call(clientWithConn(t, raw))
			require.NoError(t, err)
			require.Equal(t, &test.want, item)
			require.Equal(t, [][]byte{[]byte(test.wire)}, raw.writes)
		})
	}
}

func TestClientDebugAndStats(t *testing.T) {
	t.Run("debug", func(t *testing.T) {
		raw := newTestConn([]byte("ME key exp=-1 la=2 cas=42 fetch=yes cls=1 size=65\r\n"))
		item, err := clientWithConn(t, raw).MetaDebug(t.Context(), []byte("key"))
		require.NoError(t, err)
		require.Equal(t, &MetaItemDebug{Key: []byte("key"), TTL: -1, LastAssessTime: 2, CAS: 42, HitBefore: true, SlabClassID: 1, Size: 65}, item)
		require.Equal(t, [][]byte{[]byte("me key\r\n")}, raw.writes)
	})
	t.Run("stats", func(t *testing.T) {
		raw := newTestConn([]byte("STAT pid 42\r\nSTAT version 1.6.37\r\nEND\r\n"))
		stats, err := clientWithConn(t, raw).Stats(t.Context())
		require.NoError(t, err)
		require.Equal(t, int64(42), stats.PID)
		require.Equal(t, "1.6.37", stats.Version)
		require.Equal(t, [][]byte{[]byte("stats\r\n")}, raw.writes)
	})
}

func TestClientCommandResponseErrors(t *testing.T) {
	for _, test := range []struct {
		name, response string
		call           func(*client) error
		wantErr        error
	}{
		{"storage status", "NOT_STORED\r\n", func(c *client) error { return c.Add(t.Context(), "key", []byte("value"), 0, 0) }, ErrNotStored},
		{"arithmetic value", "invalid\r\n", func(c *client) error { _, err := c.Incr(t.Context(), "key", 1); return err }, ErrMalformedResponse},
		{"meta miss", "EN\r\n", func(c *client) error { _, err := c.MetaGet(t.Context(), []byte("key")); return err }, ErrNotFound},
		{"debug miss", "EN\r\n", func(c *client) error { _, err := c.MetaDebug(t.Context(), []byte("key")); return err }, ErrNotFound},
	} {
		t.Run(test.name, func(t *testing.T) {
			require.ErrorIs(t, test.call(clientWithConn(t, newTestConn([]byte(test.response)))), test.wantErr)
		})
	}
}

func TestClientCodecTransformsValuesAndFlags(t *testing.T) {
	newCodec := func(t *testing.T) Codec {
		return testCodec{
			Codec: memcodec.Noop,
			encode: func(key, value []byte, flags uint32) ([]byte, uint32, error) {
				require.Equal(t, []byte("key"), key)
				require.Equal(t, []byte("value"), value)
				require.Equal(t, uint32(7), flags)
				return []byte("wire"), 99, nil
			},
			decode: func(key, value []byte, flags uint32) ([]byte, uint32, error) {
				require.Equal(t, []byte("key"), key)
				require.Equal(t, []byte("wire"), value)
				require.Equal(t, uint32(99), flags)
				return []byte("value"), 7, nil
			},
		}
	}
	t.Run("classic", func(t *testing.T) {
		raw := newTestConn()
		raw.respond = func(request []byte) ([]byte, error) {
			if bytes.HasPrefix(request, []byte("set ")) {
				return []byte("STORED\r\n"), nil
			}
			return []byte("VALUE key 99 4\r\nwire\r\nEND\r\n"), nil
		}
		c := clientWithConn(t, raw, WithCodec(newCodec(t)))
		require.NoError(t, c.Set(t.Context(), "key", []byte("value"), 7, 0))
		item, err := c.Get(t.Context(), "key")
		require.NoError(t, err)
		require.Equal(t, &Item{Key: "key", Value: []byte("value"), Flags: 7}, item)
		require.Equal(t, [][]byte{[]byte("set key 99 0 4\r\nwire\r\n"), []byte("get key\r\n")}, raw.writes)
	})
	t.Run("meta", func(t *testing.T) {
		raw := newTestConn()
		raw.respond = func(request []byte) ([]byte, error) {
			if bytes.HasPrefix(request, []byte("ms ")) {
				return []byte("HD\r\n"), nil
			}
			return []byte("VA 4 f99\r\nwire\r\n"), nil
		}
		c := clientWithConn(t, raw, WithCodec(newCodec(t)))
		stored, err := c.MetaSet(t.Context(), []byte("key"), []byte("value"), MetaSetFlagClientFlags(7))
		require.NoError(t, err)
		require.Equal(t, uint32(7), stored.Flags)
		item, err := c.MetaGet(t.Context(), []byte("key"), MetaGetFlagReturnValue())
		require.NoError(t, err)
		require.Equal(t, []byte("value"), item.Value)
		require.Equal(t, uint32(7), item.Flags)
		require.Equal(t, [][]byte{[]byte("ms key 4 F99\r\nwire\r\n"), []byte("mg key f v\r\n")}, raw.writes)
	})
}

type testCodec struct {
	Codec
	encode func([]byte, []byte, uint32) ([]byte, uint32, error)
	decode func([]byte, []byte, uint32) ([]byte, uint32, error)
}

func (c testCodec) Encode(key, value []byte, flags uint32) ([]byte, uint32, error) {
	return c.encode(key, value, flags)
}

func (c testCodec) Decode(key, value []byte, flags uint32) ([]byte, uint32, error) {
	return c.decode(key, value, flags)
}

func TestClientEmitsMetricsForFailedInitialization(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		failure := errors.New("initial discovery unavailable")
		meter := &testutil.CaptureMeter{}
		r := resolverFunc(func(context.Context, string) (resolver.ResolveResult, *time.Time, error) {
			time.Sleep(100 * time.Millisecond)
			return resolver.ResolveResult{}, nil, failure
		})
		c, err := NewWithContext(t.Context(), "http://directory.example/nodes",
			WithResolver(r), WithTelemetry(telemetry.WithMeterProvider(meter.Provider())))
		require.ErrorIs(t, err, failure)
		require.Nil(t, c)
		require.Equal(t, []float64{1}, meter.Values("memcached.discovery.resolve.calls"))
		require.Equal(t, []float64{1}, meter.Values("memcached.discovery.resolve.errors"))
		require.Equal(t, []float64{0.1}, meter.Values("memcached.discovery.resolve.duration"))
		for _, name := range []string{
			"memcached.discovery.resolve.last_success",
			"memcached.topology.nodes",
			"memcached.topology.generation",
		} {
			require.Empty(t, meter.Values(name), "failed initialization must not emit %s", name)
		}
	})
}

// TestMemcachedIntegration checks compatibility with a real daemon. Set
// MEMCACHED_TEST_ADDR and select it with -run '^TestMemcachedIntegration$'.
func TestMemcachedIntegration(t *testing.T) {
	addr := os.Getenv("MEMCACHED_TEST_ADDR")
	if addr == "" {
		t.Skip("set MEMCACHED_TEST_ADDR to run against a dedicated Memcached endpoint (host:port)")
	}
	_, _, err := net.SplitHostPort(addr)
	require.NoError(t, err, "MEMCACHED_TEST_ADDR must contain a single host:port")

	c := newIntegrationClient(t, addr)
	prefix := "memcached-integration-" + rand.Text()
	key := func(t *testing.T, name string) string {
		t.Helper()
		k := prefix + "-" + name
		t.Cleanup(func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := c.Delete(ctx, k); err != nil && !errors.Is(err, ErrNotFound) {
				t.Errorf("delete integration key %q: %v", k, err)
			}
		})
		return k
	}

	t.Run("classic", func(t *testing.T) {
		ctx := t.Context()
		first, second := key(t, "classic-first"), key(t, "classic-second")
		const flags = uint32(0x12345678)
		_, err := c.Get(ctx, first)
		require.ErrorIs(t, err, ErrNotFound)
		require.NoError(t, c.Set(ctx, first, []byte("middle"), flags, 10*time.Minute))
		require.ErrorIs(t, c.Add(ctx, first, []byte("wrong"), flags, 0), ErrNotStored)
		require.NoError(t, c.Replace(ctx, first, []byte("value"), flags, 10*time.Minute))
		require.NoError(t, c.Prepend(ctx, first, []byte("before-"), flags, 0))
		require.NoError(t, c.Append(ctx, first, []byte("-after"), flags, 0))
		require.NoError(t, c.Add(ctx, second, []byte("second"), flags, 10*time.Minute))
		values := map[string][]byte{first: []byte("before-value-after"), second: []byte("second")}
		assertItems := func(items []*Item, withCAS bool) {
			t.Helper()
			require.Len(t, items, len(values))
			seen := make(map[string]bool)
			for _, item := range items {
				require.NotNil(t, item)
				require.Contains(t, values, item.Key)
				require.False(t, seen[item.Key], "duplicate result key")
				seen[item.Key] = true
				require.Equal(t, values[item.Key], item.Value)
				require.Equal(t, flags, item.Flags)
				if withCAS {
					require.NotZero(t, item.CAS)
				}
			}
		}
		item, err := c.Get(ctx, first)
		require.NoError(t, err)
		require.Equal(t, values[first], item.Value)
		require.Equal(t, flags, item.Flags)
		items, err := c.Gets(ctx, first, second)
		require.NoError(t, err)
		assertItems(items, true)

		item, err = c.GetAndTouch(ctx, 5*time.Minute, first)
		require.NoError(t, err)
		require.Equal(t, values[first], item.Value)
		require.Equal(t, flags, item.Flags)
		meta, err := c.MetaGet(ctx, []byte(first), MetaGetFlagReturnTTL())
		require.NoError(t, err)
		require.InDelta(t, 300, meta.TTL, 10, "gat must update the server expiration")
		items, err = c.GetAndTouches(ctx, 4*time.Minute, first, second)
		require.NoError(t, err)
		assertItems(items, true)
		for _, k := range []string{first, second} {
			meta, err = c.MetaGet(ctx, []byte(k), MetaGetFlagReturnTTL())
			require.NoError(t, err)
			require.InDelta(t, 240, meta.TTL, 10, "gats must update each server expiration")
		}

		cas := items[0].CAS
		require.NoError(t, c.Cas(ctx, items[0].Key, []byte("updated"), flags, time.Minute, cas))
		require.ErrorIs(t, c.Cas(ctx, items[0].Key, []byte("stale"), flags, time.Minute, cas), ErrExists)
		item, err = c.Get(ctx, items[0].Key)
		require.NoError(t, err)
		require.Equal(t, []byte("updated"), item.Value)

		counter := key(t, "classic-counter")
		require.NoError(t, c.Set(ctx, counter, []byte("10"), 0, time.Minute))
		n, err := c.Incr(ctx, counter, 7)
		require.NoError(t, err)
		require.EqualValues(t, 17, n)
		n, err = c.Decr(ctx, counter, 20)
		require.NoError(t, err)
		require.Zero(t, n, "decrement must saturate at zero")
		require.NoError(t, c.Delete(ctx, first))
		_, err = c.Get(ctx, first)
		require.ErrorIs(t, err, ErrNotFound)
		require.ErrorIs(t, c.Delete(ctx, first), ErrNotFound)
	})

	t.Run("meta", func(t *testing.T) {
		ctx := t.Context()
		k := []byte(key(t, "meta"))
		value := []byte("meta value")
		const flags = uint32(0x23456789)
		stored, err := c.MetaSet(ctx, k, value, MetaSetFlagClientFlags(flags), MetaSetFlagReturnKey(), MetaSetFlagReturnCAS())
		require.NoError(t, err)
		require.Equal(t, k, stored.Key)
		require.Equal(t, flags, stored.Flags)
		require.NotZero(t, stored.CAS)
		item, err := c.MetaGet(ctx, k, MetaGetFlagReturnValue(), MetaGetFlagReturnClientFlags(),
			MetaGetFlagReturnCAS(), MetaGetFlagReturnKey(), MetaGetFlagReturnSize(), MetaGetFlagReturnTTL(), MetaGetFlagOpaque(42))
		require.NoError(t, err)
		require.Equal(t, k, item.Key)
		require.Equal(t, value, item.Value)
		require.Equal(t, flags, item.Flags)
		require.Equal(t, stored.CAS, item.CAS)
		require.EqualValues(t, len(value), item.Size)
		require.EqualValues(t, -1, item.TTL)
		require.EqualValues(t, 42, item.Opaque)
		_, err = c.MetaDelete(ctx, k)
		require.NoError(t, err)
		_, err = c.MetaGet(ctx, k, MetaGetFlagReturnValue())
		require.ErrorIs(t, err, ErrNotFound)
	})

	t.Run("compression", func(t *testing.T) {
		ctx := t.Context()
		codec, err := memcodec.NewCompressCodec(memcodec.CompressionAlgorithmSnappy, 1, 0)
		require.NoError(t, err)
		compressed := newIntegrationClient(t, addr, WithCodec(codec))
		first, second := key(t, "compressed-first"), key(t, "compressed-second")
		value := bytes.Repeat([]byte("hello compression "), 64)
		const flags = uint32(0x1234)
		for _, k := range []string{first, second} {
			require.NoError(t, compressed.Set(ctx, k, value, flags, time.Minute))
		}
		raw, err := c.Get(ctx, first)
		require.NoError(t, err)
		require.True(t, memcodec.IsCompressed(raw.Flags))
		require.Equal(t, flags, memcodec.AppFlags(raw.Flags))
		require.Less(t, len(raw.Value), len(value), "the daemon must store compressed bytes")
		assertItem := func(item *Item) {
			t.Helper()
			require.NotNil(t, item)
			require.Equal(t, value, item.Value)
			require.Equal(t, flags, item.Flags)
		}
		item, err := compressed.Get(ctx, first)
		require.NoError(t, err)
		assertItem(item)
		items, err := compressed.Gets(ctx, first, second)
		require.NoError(t, err)
		require.Len(t, items, 2)
		for _, item := range items {
			assertItem(item)
			require.NotZero(t, item.CAS)
		}
		item, err = compressed.GetAndTouch(ctx, time.Minute, first)
		require.NoError(t, err)
		assertItem(item)
		items, err = compressed.GetAndTouches(ctx, time.Minute, first, second)
		require.NoError(t, err)
		require.Len(t, items, 2)
		for _, item := range items {
			assertItem(item)
			require.NotZero(t, item.CAS)
		}

		metaKey := []byte(key(t, "compressed-meta"))
		stored, err := compressed.MetaSet(ctx, metaKey, value, MetaSetFlagClientFlags(flags))
		require.NoError(t, err)
		require.Equal(t, flags, stored.Flags)
		rawMeta, err := c.MetaGet(ctx, metaKey, MetaGetFlagReturnValue(), MetaGetFlagReturnClientFlags())
		require.NoError(t, err)
		require.True(t, memcodec.IsCompressed(rawMeta.Flags))
		require.Equal(t, flags, memcodec.AppFlags(rawMeta.Flags))
		require.Less(t, len(rawMeta.Value), len(value))
		meta, err := compressed.MetaGet(ctx, metaKey, MetaGetFlagReturnValue())
		require.NoError(t, err)
		require.Equal(t, value, meta.Value)
		require.Equal(t, flags, meta.Flags, "codec flags must be fetched even when callers do not request them")
	})

	t.Run("concurrent classic reads", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
		defer cancel()
		jobs := make([]func() error, 8)
		for i := range jobs {
			k := key(t, fmt.Sprintf("concurrent-read-%d", i))
			require.NoError(t, c.Set(ctx, k, []byte(k), 0, time.Minute))
			jobs[i] = func() error {
				for range 25 {
					item, err := c.Get(ctx, k)
					if err != nil {
						return fmt.Errorf("get %q: %w", k, err)
					}
					if item == nil || string(item.Value) != k {
						return fmt.Errorf("get %q: unexpected item %+v", k, item)
					}
				}
				return nil
			}
		}
		runIntegrationWorkers(t, jobs...)
	})

	// Regression for https://github.com/yeqown/memcached/issues/18: sharing a
	// client across MetaSet, Get and Touch must preserve pooled response state.
	t.Run("concurrent meta set get touch", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
		defer cancel()
		k := key(t, "concurrent-meta")
		value := []byte("concurrent meta value")
		const flags = uint32(0x1234)
		options := []MetaSetOption{MetaSetFlagClientFlags(flags), MetaSetFlagTTL(60), MetaSetFlagReturnKey(), MetaSetFlagReturnCAS()}
		initial, err := c.MetaSet(ctx, []byte(k), value, options...)
		require.NoError(t, err)
		runIntegrationWorkers(t,
			func() error {
				for range 32 {
					item, err := c.MetaSet(ctx, []byte(k), value, append(options, MetaSetFlagNewCAS(initial.CAS))...)
					if err != nil {
						return fmt.Errorf("meta set: %w", err)
					}
					if item == nil || string(item.Key) != k || item.Flags != flags || item.CAS == 0 {
						return fmt.Errorf("meta set: unexpected item %+v", item)
					}
				}
				return nil
			},
			func() error {
				for range 64 {
					item, err := c.Get(ctx, k)
					if err != nil {
						return fmt.Errorf("get: %w", err)
					}
					if item == nil || !bytes.Equal(value, item.Value) || item.Flags != flags {
						return fmt.Errorf("get: unexpected item %+v", item)
					}
				}
				return nil
			},
			func() error {
				for range 32 {
					if err := c.Touch(ctx, k, time.Minute); err != nil {
						return fmt.Errorf("touch: %w", err)
					}
				}
				return nil
			},
		)
		item, err := c.Get(ctx, k)
		require.NoError(t, err)
		require.Equal(t, value, item.Value)
		require.Equal(t, flags, item.Flags)
	})
}

func newIntegrationClient(t *testing.T, addr string, options ...ClientOption) Client {
	t.Helper()
	c, err := New(addr, options...)
	require.NoError(t, err)
	t.Cleanup(func() {
		if err := c.Close(); err != nil {
			t.Errorf("close integration client: %v", err)
		}
	})
	return c
}

func runIntegrationWorkers(t *testing.T, jobs ...func() error) {
	t.Helper()
	var wg sync.WaitGroup
	start := make(chan struct{})
	errs := make(chan error, len(jobs))
	for _, job := range jobs {
		wg.Go(func() {
			<-start
			if err := job(); err != nil {
				errs <- err
			}
		})
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

func newTestClient(t *testing.T, addrs []*resolver.Addr, opts ...ClientOption) *client {
	t.Helper()
	r := WithResolver(resolverFunc(func(context.Context, string) (resolver.ResolveResult, *time.Time, error) {
		return resolver.ResolveResult{Addrs: addrs, Generation: 1}, nil, nil
	}))
	cc, err := New("directory", append([]ClientOption{r}, opts...)...)
	require.NoError(t, err)
	c := cc.(*client)
	for _, n := range c.topology.nodes {
		n.pool.createConn = createTestConn
	}
	t.Cleanup(func() { require.NoError(t, c.Close()) })
	return c
}

func clientWithConn(t *testing.T, cn *testConn, opts ...ClientOption) *client {
	t.Helper()
	addr := resolver.NewAddr("tcp", "cache:11211", 0)
	c := newTestClient(t, []*resolver.Addr{addr}, opts...)
	c.topology.nodes[addr.AddrKey].pool.createConn = func(context.Context) (memcachedConn, error) { return cn, nil }
	return c
}

func clientView(c *client) topologySnapshot { return topologyView(c.topology) }

func mustCompressCodec(t *testing.T, algorithm memcodec.Compression, threshold, level int) memcodec.CompressCodec {
	t.Helper()
	codec, err := memcodec.NewCompressCodec(algorithm, threshold, level)
	require.NoError(t, err)
	return codec
}

func TestClientWiresTopologyMetrics(t *testing.T) {
	meter := &testutil.CaptureMeter{}
	c := newTestClient(t, []*resolver.Addr{resolver.NewAddr("tcp", "cache:11211", 0)}, WithTelemetry(telemetry.WithMeterProvider(meter.Provider())))
	require.NotNil(t, c.metrics)
	require.NotNil(t, c.topology.metrics)
	require.Equal(t, []float64{1}, meter.Values("memcached.discovery.resolve.calls"))
	require.Equal(t, []float64{1}, meter.Values("memcached.topology.nodes"))
}
