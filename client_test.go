package memcached

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	pkgerrors "github.com/pkg/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"

	memcodec "github.com/yeqown/memcached/codec"
	"github.com/yeqown/memcached/resolver"
	"github.com/yeqown/memcached/telemetry"
)

type clientTestSuite struct {
	suite.Suite

	client  *client
	address string
}

func mustCompressCodec(t *testing.T, algorithm memcodec.Compression, threshold, level int) memcodec.CompressCodec {
	t.Helper()
	codec, err := memcodec.NewCompressCodec(algorithm, threshold, level)
	require.NoError(t, err)
	return codec
}

func (su *clientTestSuite) SetupTest() {
	t := su.T()
	su.address = startClientTestServer(t)
	c, err := NewWithContext(t.Context(), su.address)
	su.Require().NoError(err)
	su.client = c.(*client)
	t.Cleanup(func() { require.NoError(t, c.Close()) })
}

func (su *clientTestSuite) newCompressedClient() *client {
	t := su.T()
	c, err := NewWithContext(
		t.Context(),
		su.address,
		WithCodec(mustCompressCodec(t, memcodec.CompressionAlgorithmDeflate, 1, 6)),
	)
	require.NoError(t, err)

	cc := c.(*client)
	t.Cleanup(func() {
		require.NoError(t, cc.Close())
	})
	return cc
}

func (su *clientTestSuite) Test_concurrent_dispatchRequest() {
	key := "Test_concurrent_dispatchRequest"
	// prepare data

	ctx, cancel := context.WithTimeout(su.T().Context(), 5*time.Second)
	defer cancel()

	err := su.client.Set(ctx, key, []byte("Test_concurrent_dispatchRequest"), 0, 0)
	su.Require().NoError(err)

	wg := sync.WaitGroup{}
	limits := 100
	errors := make(chan error, 10)
	for range 10 {
		wg.Go(func() {
			for range limits {
				req, resp := buildGetsCommand("get", key)
				err := su.client.dispatchRequest(ctx, req, resp)
				releaseReqAndResp(req, resp)
				if err != nil {
					errors <- err
					return
				}
			}
		})
	}

	wg.Wait()
	close(errors)
	for err := range errors {
		su.Require().NoError(err)
	}
}

// https://github.com/yeqown/memcached/issues/18
// Mock a concurrent client to set, get and touch the cache at the same time.
func (su *clientTestSuite) Test_concurrent() {
	// metaset options
	msOptions := func(
		cas uint64, clientFlags uint32,
	) []MetaSetOption {
		expiration := 2
		return []MetaSetOption{
			MetaSetFlagNewCAS(cas),
			MetaSetFlagTTL(uint64(expiration)),
			MetaSetFlagClientFlags(clientFlags),
			MetaSetFlagReturnKey(),
			MetaSetFlagReturnSize(),
			MetaSetFlagReturnCAS(),
		}
	}

	key := "Test_concurrent"
	value := "Test_concurrent is value of Test_concurrent"
	// prepare data
	ctx, cancel := context.WithTimeout(su.T().Context(), 5*time.Second)
	defer cancel()

	initial, err := su.client.MetaSet(ctx, []byte(key), []byte(value), msOptions(0, 0x1234)...)
	su.Require().NoError(err)

	wg := sync.WaitGroup{}
	wg.Add(3)

	// meta set goroutine
	go func() {
		defer wg.Done()

		counter := 0
		cas := initial.CAS

		// update only
		for counter <= 20 {
			item, err := su.client.MetaSet(ctx, []byte(key), []byte(value), msOptions(cas, 0x1234)...)
			su.NoError(err)
			if err != nil {
				return
			}

			cas = item.CAS
			counter++

			time.Sleep(20 * time.Millisecond)
		}
	}()

	// get goroutine
	go func() {
		defer wg.Done()

		counter := 0

		for counter <= 200 {
			item, err := su.client.Get(ctx, key)
			su.NoError(err)
			if err != nil {
				return
			}

			su.Equal(value, string(item.Value))
			counter++

			time.Sleep(5 * time.Millisecond)
		}
	}()

	// touch goroutine
	go func() {
		defer wg.Done()
		counter := 0

		for counter <= 100 {
			err := su.client.Touch(ctx, key, 3*time.Second)
			su.NoError(err)
			if err != nil {
				return
			}

			counter++

			time.Sleep(10 * time.Millisecond)
		}
	}()

	wg.Wait()

	item, err := su.client.Get(ctx, key)
	su.Require().NoError(err)
	su.Equal(value, string(item.Value))
	su.T().Log("Test_concurrent finished")
}

func (su *clientTestSuite) Test_compressionClassicReadCommandsRoundTrip() {
	ctx := context.Background()
	client := su.newCompressedClient()

	value := []byte("hello hello hello hello hello hello")
	flag := uint32(0x1234)
	key1 := "Test_compressionClassicReadCommandsRoundTrip_1"
	key2 := "Test_compressionClassicReadCommandsRoundTrip_2"

	su.Require().NoError(client.Set(ctx, key1, value, flag, 0))
	su.Require().NoError(client.Set(ctx, key2, value, flag, 0))
	raw, err := su.client.Get(ctx, key1)
	su.Require().NoError(err)
	su.True(memcodec.IsCompressed(raw.Flags), "the server must receive compressed wire data")
	su.Equal(flag, memcodec.AppFlags(raw.Flags))
	su.NotEqual(value, raw.Value)

	assertItem := func(item *Item) {
		su.Require().NotNil(item)
		su.Equal(value, item.Value)
		su.Equal(flag, item.Flags)
	}

	item, err := client.Get(ctx, key1)
	su.Require().NoError(err)
	assertItem(item)

	items, err := client.Gets(ctx, key1, key2)
	su.Require().NoError(err)
	su.Require().Len(items, 2)
	for _, item := range items {
		assertItem(item)
		su.NotZero(item.CAS)
	}

	item, err = client.GetAndTouch(ctx, time.Second, key1)
	su.Require().NoError(err)
	assertItem(item)

	items, err = client.GetAndTouches(ctx, time.Second, key1, key2)
	su.Require().NoError(err)
	su.Require().Len(items, 2)
	for _, item := range items {
		assertItem(item)
		su.NotZero(item.CAS)
	}
}

func (su *clientTestSuite) Test_compressionMetaReadTransparency() {
	ctx := context.Background()
	client := su.newCompressedClient()

	key := []byte("Test_compressionMetaReadTransparency")
	value := []byte("hello hello hello hello hello hello")
	flag := uint32(0x2345)

	stored, err := client.MetaSet(ctx, key, value, MetaSetFlagClientFlags(flag))
	su.Require().NoError(err)
	su.Equal(flag, stored.Flags)
	raw, err := su.client.MetaGet(ctx, key, MetaGetFlagReturnValue(), MetaGetFlagReturnClientFlags())
	su.Require().NoError(err)
	su.True(memcodec.IsCompressed(raw.Flags), "meta set must send compressed wire data")
	su.Equal(flag, memcodec.AppFlags(raw.Flags))
	su.NotEqual(value, raw.Value)

	item, err := client.MetaGet(ctx, key, MetaGetFlagReturnValue())
	su.Require().NoError(err)
	su.Equal(value, item.Value)
	su.Equal(flag, item.Flags)

	item, err = client.MetaGet(ctx, key, MetaGetFlagReturnValue(), MetaGetFlagReturnClientFlags())
	su.Require().NoError(err)
	su.Equal(value, item.Value)
	su.Equal(flag, item.Flags)
}

func TestCompressionDisablesAppendPrepend(t *testing.T) {
	client := &client{options: newClientOptions()}
	client.options.codec = mustCompressCodec(t, memcodec.CompressionAlgorithmDeflate, 0, 6)

	err := client.Append(context.Background(), "key", []byte("value"), 0, 0)
	require.Error(t, err)
	assert.True(t, pkgerrors.Is(err, ErrNotSupported))

	err = client.Prepend(context.Background(), "key", []byte("value"), 0, 0)
	require.Error(t, err)
	assert.True(t, pkgerrors.Is(err, ErrNotSupported))
}

func TestCompressionDisablesMetaAppendPrepend(t *testing.T) {
	client := &client{options: newClientOptions()}
	client.options.codec = mustCompressCodec(t, memcodec.CompressionAlgorithmDeflate, 0, 6)

	errModes := []metaSetMode{MetaSetModeAppend, MetaSetModePrepend}
	for _, mode := range errModes {
		t.Run(string(mode), func(t *testing.T) {
			item, err := client.MetaSet(
				context.Background(),
				[]byte("key"),
				[]byte("value"),
				MetaSetFlagModeSwitch(mode),
			)
			assert.Nil(t, item)
			require.Error(t, err)
			assert.True(t, pkgerrors.Is(err, ErrNotSupported))
		})
	}
}

type prependOnlyRestrictedCodec struct{}

func (prependOnlyRestrictedCodec) Encode(_ []byte, value []byte, flags uint32) ([]byte, uint32, error) {
	return value, flags, nil
}

func (prependOnlyRestrictedCodec) Decode(_ []byte, value []byte, flags uint32) ([]byte, uint32, error) {
	return value, flags, nil
}

func (prependOnlyRestrictedCodec) SupportsOperation(operation string) error {
	if operation == "prepend" || operation == "cas" {
		return ErrNotSupported
	}
	return nil
}

func TestCodecCapabilitiesApplyPerTextStorageOperation(t *testing.T) {
	codec := prependOnlyRestrictedCodec{}

	require.NoError(t, checkCodecSupportsOperation(codec, "append"))
	require.ErrorIs(t, checkCodecSupportsOperation(codec, "prepend"), ErrNotSupported)
}

func TestCodecCapabilitiesApplyPerCasOperation(t *testing.T) {
	c := &client{options: newClientOptions()}
	c.options.codec = prependOnlyRestrictedCodec{}

	_, _, err := buildCasCommand("foo", []byte("bar"), 0, 0, 1, false, c.options.codec)
	require.ErrorIs(t, err, ErrNotSupported)
}

func TestCodecCapabilitiesApplyPerMetaSetOperation(t *testing.T) {
	c := &client{options: newClientOptions()}
	c.options.codec = prependOnlyRestrictedCodec{}

	flags := &metaSetFlags{}
	MetaSetFlagModeSwitch(MetaSetModeAppend)(flags)
	_, _, err := buildMetaSetCommand([]byte("foo"), []byte("bar"), flags, c.options.codec)
	require.NoError(t, err)

	flags = &metaSetFlags{}
	MetaSetFlagModeSwitch(MetaSetModePrepend)(flags)
	_, _, err = buildMetaSetCommand([]byte("foo"), []byte("bar"), flags, c.options.codec)
	require.ErrorIs(t, err, ErrNotSupported)
}

func TestNewCompressCodecInstallsCompressionBehavior(t *testing.T) {
	codec := mustCompressCodec(t, memcodec.CompressionAlgorithmDeflate, 1, 6)

	encodedValue, encodedFlags, err := codec.Encode([]byte("foo"), []byte("hello hello hello hello hello hello"), 0x12)
	require.NoError(t, err)
	assert.NotEqual(t, []byte("hello hello hello hello hello hello"), encodedValue)
	assert.True(t, memcodec.IsCompressed(encodedFlags))
	assert.Equal(t, uint32(0x12), memcodec.AppFlags(encodedFlags))
}

func TestClientSuite(t *testing.T) {
	suite.Run(t, new(clientTestSuite))
}

func TestClientBroadcastRejectsRemovedUnborrowedTarget(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		a, b := resolver.NewAddr("tcp", "a:11211", 0), resolver.NewAddr("tcp", "b:11211", 0)
		c := topologyClient(t, []*resolver.Addr{a, b})
		initial := clientView(c)
		first := initial.nodes[resolver.AddrKey{Network: "tcp", Address: "a:11211"}]
		second := initial.nodes[resolver.AddrKey{Network: "tcp", Address: "b:11211"}]
		started, proceed := make(chan struct{}), make(chan struct{})
		rawA := &nodeLifecycleConn{newPoolLifecycleConn()}
		rawB := &nodeLifecycleConn{newPoolLifecycleConn()}
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
		close(proceed)
		require.Error(t, <-done, "removal rejects a target that is still dialing")
		require.Empty(t, called, "removed and newly added targets must not execute")
		require.EqualValues(t, 1, rawA.closes.Load())
		requirePoolClosed(t, first.pool)
		require.Zero(t, rawB.closes.Load(), "the retained node keeps its idle connection")
		require.Zero(t, clientView(c).nodes[resolver.AddrKey{Network: "tcp", Address: "c:11211"}].pool.stats().TotalConns)
	})
}

func TestClientPickConnReleaseDrainsNodeOnce(t *testing.T) {
	a := resolver.NewAddr("tcp", "a:11211", 0)
	c := topologyClient(t, []*resolver.Addr{a})
	n := clientView(c).nodes[a.AddrKey]
	raw := &nodeLifecycleConn{newPoolLifecycleConn()}
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
				c := topologyClient(t, []*resolver.Addr{a}, WithMaxConns(1))
				n := clientView(c).nodes[a.AddrKey]
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				failure := pkgerrors.New("dial failed")
				proceed := make(chan struct{})
				var releaseOwner func(error)
				if stage == "interrupted pool wait" {
					n.pool.createConn = func(context.Context) (memcachedConn, error) {
						return &nodeLifecycleConn{newPoolLifecycleConn()}, nil
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
				c := topologyClient(t, []*resolver.Addr{a})
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
					return &nodeLifecycleConn{newPoolLifecycleConn()}, nil
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

func TestClientDispatchRecordsReceiveError(t *testing.T) {
	a := resolver.NewAddr("tcp", "a:11211", 0)
	c := topologyClient(t, []*resolver.Addr{a})
	span := &requestTraceSpan{Span: trace.SpanFromContext(t.Context())}
	provider := &requestTraceProvider{
		TracerProvider: noop.NewTracerProvider(),
		tracer:         &requestTracer{Tracer: noop.NewTracerProvider().Tracer("test"), span: span},
	}
	c.tracer = telemetry.NewConfig(telemetry.WithTracerProvider(provider)).Tracer()
	failure := pkgerrors.New("receive failed")
	raw := &receiveErrorConn{nodeLifecycleConn: &nodeLifecycleConn{newPoolLifecycleConn()}, err: failure}
	n := clientView(c).nodes[a.AddrKey]
	n.pool.createConn = func(context.Context) (memcachedConn, error) { return raw, nil }
	_, err := c.Version(t.Context())
	require.ErrorIs(t, err, failure)
	require.ErrorIs(t, span.err, failure)
	require.Equal(t, codes.Error, span.status)
	require.Equal(t, 1, span.ends)
	require.Equal(t, 1, n.pool.stats().IdleConns)
}

type receiveErrorConn struct {
	*nodeLifecycleConn
	err error
}

func (c *receiveErrorConn) Write(p []byte) (int, error) { return len(p), nil }

func (c *receiveErrorConn) readLine(byte) ([]byte, error) { return nil, c.err }

func TestClientRequestsKeepSelectedNode(t *testing.T) {
	gate := make(chan struct{})
	release := sync.OnceFunc(func() { close(gate) })
	defer release()
	a, b := startDiscoveryNode(t, "a", gate), startDiscoveryNode(t, "b")
	c := topologyClient(t, []*resolver.Addr{resolver.NewAddr("tcp", a.address, 0)})
	done := make(chan string, 1)
	go func() {
		v, err := c.Version(context.Background())
		if err != nil {
			v = err.Error()
		}
		done <- v
	}()
	select {
	case <-a.entered:
	case <-time.After(time.Second):
		t.Fatal("request did not reach old node")
	}
	require.NoError(t, updateTopology(c, []*resolver.Addr{resolver.NewAddr("tcp", b.address, 0)}))
	require.Equal(t, b.address, clientView(c).addrs[0].Address, "the refreshed topology must route new requests to the new node")
	v, err := c.Version(context.Background())
	require.NoError(t, err)
	require.Equal(t, "b", v)
	release()
	require.Equal(t, "a", <-done)
	require.NoError(t, c.FlushAll(context.Background()))
	require.Equal(t, int32(0), a.flushes.Load())
	require.Equal(t, int32(1), b.flushes.Load())
}

type retrievalPicker struct{}

func (retrievalPicker) Pick(addrs []*resolver.Addr, _, key []byte) (*resolver.Addr, error) {
	if string(key) == "route-to-second" {
		return addrs[len(addrs)-1], nil
	}
	return addrs[0], nil
}

func TestClientRetrievalUsesKeyForRouting(t *testing.T) {
	a, b := startDiscoveryNode(t, "a"), startDiscoveryNode(t, "b")
	c := topologyClient(t, []*resolver.Addr{resolver.NewAddr("tcp", a.address, 0), resolver.NewAddr("tcp", b.address, 0)}, WithPicker(retrievalPicker{}))
	ctx := context.Background()
	require.NoError(t, c.Set(ctx, "route-to-second", []byte("value"), 0, 0))
	for _, cmd := range []string{"get", "gets", "gat", "gats"} {
		t.Run(cmd, func(t *testing.T) {
			var item *Item
			var items []*Item
			var err error
			switch cmd {
			case "get":
				item, err = c.Get(ctx, "route-to-second")
			case "gets":
				items, err = c.Gets(ctx, "route-to-second")
			case "gat":
				item, err = c.GetAndTouch(ctx, time.Minute, "route-to-second")
			case "gats":
				items, err = c.GetAndTouches(ctx, time.Minute, "route-to-second")
			}
			require.NoError(t, err)
			if item == nil {
				require.Len(t, items, 1)
				item = items[0]
			}
			require.Equal(t, []byte("value"), item.Value)
		})
	}
}

func TestClientTopologyBroadcastKeepsOriginalTargets(t *testing.T) {
	a, b, d := startDiscoveryNode(t, "a"), startDiscoveryNode(t, "b"), startDiscoveryNode(t, "d")
	c := topologyClient(t, []*resolver.Addr{resolver.NewAddr("tcp", a.address, 0), resolver.NewAddr("tcp", b.address, 0)})
	gate := make(chan struct{})
	release := sync.OnceFunc(func() { close(gate) })
	defer release()
	entered := make(chan string, 2)
	done := make(chan error, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	go func() {
		done <- c.broadcastRequest(ctx, func(ctx context.Context, cn memcachedConn) error {
			entered <- cn.(*conn).addr.String()
			select {
			case <-gate:
			case <-ctx.Done():
				return ctx.Err()
			}
			if _, err := cn.Write([]byte("flush_all\r\n")); err != nil {
				return err
			}
			_, err := cn.readLine('\n')
			return err
		})
	}()
	var targets []string
	for range 2 {
		select {
		case addr := <-entered:
			targets = append(targets, addr)
		case <-ctx.Done():
			t.Fatal("broadcast did not borrow both original connections")
		}
	}
	require.ElementsMatch(t, []string{a.address, b.address}, targets)
	initial := clientView(c)
	require.NoError(t, updateTopology(c, []*resolver.Addr{resolver.NewAddr("tcp", b.address, 0), resolver.NewAddr("tcp", d.address, 0)}))
	release()
	require.NoError(t, <-done)
	require.Equal(t, int32(1), a.flushes.Load())
	require.Equal(t, int32(1), b.flushes.Load())
	require.Zero(t, d.flushes.Load())
	requirePoolClosed(t, initial.nodes[resolver.AddrKey{Network: "tcp", Address: a.address}].pool)
	require.NoError(t, c.FlushAll(ctx))
	require.Equal(t, int32(1), a.flushes.Load())
	require.Equal(t, int32(2), b.flushes.Load())
	require.Equal(t, int32(1), d.flushes.Load())
}

type testPickerFunc func([]*resolver.Addr, []byte, []byte) (*resolver.Addr, error)

func (p testPickerFunc) Pick(addrs []*resolver.Addr, cmd, key []byte) (*resolver.Addr, error) {
	return p(addrs, cmd, key)
}

func TestClientRejectsPickerOutsideActiveAddresses(t *testing.T) {
	for name, result := range map[string]*resolver.Addr{"nil": nil, "outside": resolver.NewAddr("tcp", "b:11211", 0)} {
		t.Run(name, func(t *testing.T) {
			picker := testPickerFunc(func([]*resolver.Addr, []byte, []byte) (*resolver.Addr, error) { return result, nil })
			c := topologyClient(t, []*resolver.Addr{resolver.NewAddr("tcp", "a:11211", 0)}, WithPicker(picker))
			_, err := c.Version(context.Background())
			require.ErrorIs(t, err, ErrInvalidAddress)
			for _, p := range clientView(c).nodes {
				require.Zero(t, p.pool.stats().TotalConns)
			}
		})
	}
}

func TestClientBroadcastContinuesAfterFailure(t *testing.T) {
	for _, stage := range []string{"borrow", "call"} {
		t.Run(stage, func(t *testing.T) {
			a, b := resolver.NewAddr("tcp", "a:11211", 0), resolver.NewAddr("tcp", "b:11211", 0)
			c := topologyClient(t, []*resolver.Addr{a, b})
			view := clientView(c)
			first, second := view.nodes[a.AddrKey], view.nodes[b.AddrKey]
			failure := errors.New("node failed")
			first.pool.createConn = func(context.Context) (memcachedConn, error) {
				if stage == "borrow" {
					return nil, failure
				}
				return &nodeLifecycleConn{newPoolLifecycleConn()}, nil
			}
			second.pool.createConn = func(context.Context) (memcachedConn, error) {
				return &nodeLifecycleConn{newPoolLifecycleConn()}, nil
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

// clientTestServer implements the text commands used by the client suite.
// It stores wire values unchanged so compression is exercised by the real client.
type clientTestServer struct {
	mu      sync.Mutex
	items   map[string]clientTestItem
	nextCAS uint64
}

type clientTestItem struct {
	value   []byte
	flags   uint32
	cas     uint64
	expires time.Time
}

func startClientTestServer(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	server := &clientTestServer{items: make(map[string]clientTestItem)}
	var connections sync.Map
	var handlers sync.WaitGroup
	acceptDone := make(chan struct{})
	go func() {
		defer close(acceptDone)
		for {
			cn, err := listener.Accept()
			if err != nil {
				return
			}
			connections.Store(cn, struct{}{})
			handlers.Go(func() {
				defer connections.Delete(cn)
				defer func() { _ = cn.Close() }()
				reader := bufio.NewReader(cn)
				for {
					line, err := reader.ReadString('\n')
					if err != nil {
						return // Clients and cleanup close these connections.
					}
					if line == "quit\r\n" {
						return
					}
					response, err := server.respond(reader, strings.Fields(line))
					if err != nil {
						t.Errorf("client test server: command %q: %v", line, err)
						return
					}
					if _, err := io.WriteString(cn, response); err != nil {
						if !errors.Is(err, net.ErrClosed) {
							t.Errorf("client test server: response: %v", err)
						}
						return
					}
				}
			})
		}
	}()
	t.Cleanup(func() {
		require.NoError(t, listener.Close())
		<-acceptDone
		connections.Range(func(key, _ any) bool {
			_ = key.(net.Conn).Close() // Interrupt handlers before waiting for them.
			return true
		})
		handlers.Wait()
	})
	return listener.Addr().String()
}

func (s *clientTestServer) respond(reader io.Reader, fields []string) (string, error) {
	if len(fields) < 2 {
		return "", fmt.Errorf("incomplete command: %v", fields)
	}
	switch fields[0] {
	case "set", "ms":
		return s.store(reader, fields)
	case "get", "gets", "gat", "gats":
		keyStart := 1
		var expires time.Time
		if fields[0] == "gat" || fields[0] == "gats" {
			if len(fields) < 3 {
				return "", fmt.Errorf("missing retrieval key: %v", fields)
			}
			var err error
			expires, err = clientTestExpiry(fields[1])
			if err != nil {
				return "", err
			}
			keyStart = 2
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		var response strings.Builder
		for _, key := range fields[keyStart:] {
			item, ok := s.lookupLocked(key)
			if !ok {
				continue
			}
			if keyStart == 2 {
				item.expires = expires
				s.items[key] = item
			}
			suffix := ""
			if fields[0] == "gets" || fields[0] == "gats" {
				suffix = " " + strconv.FormatUint(item.cas, 10)
			}
			fmt.Fprintf(&response, "VALUE %s %d %d%s\r\n%s\r\n", key, item.flags, len(item.value), suffix, item.value)
		}
		response.WriteString("END\r\n")
		return response.String(), nil
	case "touch":
		if len(fields) != 3 {
			return "", fmt.Errorf("invalid touch command: %v", fields)
		}
		expires, err := clientTestExpiry(fields[2])
		if err != nil {
			return "", err
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		item, ok := s.lookupLocked(fields[1])
		if !ok {
			return "NOT_FOUND\r\n", nil
		}
		item.expires = expires
		s.items[fields[1]] = item
		return "TOUCHED\r\n", nil
	case "mg":
		s.mu.Lock()
		defer s.mu.Unlock()
		item, ok := s.lookupLocked(fields[1])
		if !ok {
			return "EN\r\n", nil
		}
		header := "HD"
		if slices.Contains(fields[2:], "v") {
			header = fmt.Sprintf("VA %d", len(item.value))
		}
		for _, flag := range fields[2:] {
			switch flag {
			case "v":
			case "f":
				header += " f" + strconv.FormatUint(uint64(item.flags), 10)
			default:
				return "", fmt.Errorf("unsupported meta get flag: %s", flag)
			}
		}
		if slices.Contains(fields[2:], "v") {
			return header + "\r\n" + string(item.value) + "\r\n", nil
		}
		return header + "\r\n", nil
	default:
		return "", fmt.Errorf("unsupported command: %s", fields[0])
	}
}

func (s *clientTestServer) store(reader io.Reader, fields []string) (string, error) {
	item := clientTestItem{}
	sizeAt := 2
	if fields[0] == "set" {
		if len(fields) != 5 {
			return "", fmt.Errorf("invalid set command: %v", fields)
		}
		flags, err := strconv.ParseUint(fields[2], 10, 32)
		if err != nil {
			return "", err
		}
		item.flags = uint32(flags)
		item.expires, err = clientTestExpiry(fields[3])
		if err != nil {
			return "", err
		}
		sizeAt = 4
	} else if len(fields) < 3 {
		return "", fmt.Errorf("invalid meta set command: %v", fields)
	}
	size, err := strconv.Atoi(fields[sizeAt])
	if err != nil || size < 0 || size > 1<<20 {
		return "", fmt.Errorf("invalid value size: %q", fields[sizeAt])
	}
	for _, flag := range fields[sizeAt+1:] {
		switch flag {
		case "c", "k", "s":
			continue
		}
		switch flag[0] {
		case 'F':
			flags, err := strconv.ParseUint(flag[1:], 10, 32)
			if err != nil {
				return "", err
			}
			item.flags = uint32(flags)
		case 'T':
			item.expires, err = clientTestExpiry(flag[1:])
			if err != nil {
				return "", err
			}
		case 'E':
			item.cas, err = strconv.ParseUint(flag[1:], 10, 64)
			if err != nil {
				return "", err
			}
		default:
			return "", fmt.Errorf("unsupported meta set flag: %s", flag)
		}
	}
	body := make([]byte, size+2)
	if _, err := io.ReadFull(reader, body); err != nil {
		return "", err
	}
	if string(body[size:]) != "\r\n" {
		return "", errors.New("value is missing CRLF")
	}
	item.value = body[:size]
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextCAS++
	if item.cas == 0 {
		item.cas = s.nextCAS
	}
	s.items[fields[1]] = item
	if fields[0] == "set" {
		return "STORED\r\n", nil
	}
	response := "HD"
	for _, flag := range fields[3:] {
		switch flag {
		case "c":
			response += " c" + strconv.FormatUint(item.cas, 10)
		case "k":
			response += " k" + fields[1]
		case "s":
			response += " s" + strconv.Itoa(size)
		}
	}
	return response + "\r\n", nil
}

func (s *clientTestServer) lookupLocked(key string) (clientTestItem, bool) {
	item, ok := s.items[key]
	if ok && !item.expires.IsZero() && !time.Now().Before(item.expires) {
		delete(s.items, key)
		return clientTestItem{}, false
	}
	return item, ok
}

func clientTestExpiry(token string) (time.Time, error) {
	seconds, err := strconv.ParseUint(token, 10, 32)
	if err != nil || seconds == 0 {
		return time.Time{}, err
	}
	if seconds > 30*24*60*60 {
		return time.Unix(int64(seconds), 0), nil
	}
	return time.Now().Add(time.Duration(seconds) * time.Second), nil
}
