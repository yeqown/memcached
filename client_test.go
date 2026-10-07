package memcached

import (
	"context"
	"errors"
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

	client *client
}

func mustCompressCodec(t *testing.T, algorithm memcodec.Compression, threshold, level int) memcodec.CompressCodec {
	t.Helper()
	codec, err := memcodec.NewCompressCodec(algorithm, threshold, level)
	require.NoError(t, err)
	return codec
}

func (su *clientTestSuite) SetupSuite() {
	addrs := "localhost:11211"
	c, err := NewWithContext(context.Background(), addrs)
	su.Require().NoError(err)
	su.client = c.(*client)
}

func (su *clientTestSuite) TearDownSuite() {
	err := su.client.Close()
	su.Require().NoError(err)
}

func (su *clientTestSuite) newCompressedClient() *client {
	c, err := NewWithContext(
		context.Background(),
		"localhost:11211",
		WithCodec(mustCompressCodec(su.T(), memcodec.CompressionAlgorithmDeflate, 1, 6)),
	)
	require.NoError(su.T(), err)

	cc := c.(*client)
	su.T().Cleanup(func() {
		require.NoError(su.T(), cc.Close())
	})
	return cc
}

func (su *clientTestSuite) Test_concurrent_dispatchRequest() {
	key := "Test_concurrent_dispatchRequest"
	// prepare data

	ctx := context.Background()

	err := su.client.Set(ctx, key, []byte("Test_concurrent_dispatchRequest"), 0, 0)
	su.Require().NoError(err)

	wg := sync.WaitGroup{}
	limits := 100
	for range 10 {
		wg.Go(func() {
			for range limits {
				req, resp := buildGetsCommand("get", key)
				err := su.client.dispatchRequest(ctx, req, resp)
				su.Require().NoError(err)
			}
		})
	}

	wg.Wait()
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
	ctx := context.Background()

	wg := sync.WaitGroup{}
	wg.Add(3)

	// meta set goroutine
	go func() {
		defer wg.Done()

		counter := 0
		cas := uint64(0)

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
			if pkgerrors.Is(err, ErrNotFound) {
				goto next
			}
			su.NoError(err)
			if err != nil {
				return
			}

			su.Equal(value, string(item.Value))
			counter++

		next:
			time.Sleep(5 * time.Millisecond)
		}
	}()

	// touch goroutine
	go func() {
		defer wg.Done()
		counter := 0

		for counter <= 100 {
			err := su.client.Touch(ctx, key, 3)
			if !pkgerrors.Is(err, ErrNotFound) {
				su.NoError(err)
			}

			counter++

			time.Sleep(10 * time.Millisecond)
		}
	}()

	wg.Wait()

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
	if testing.Short() {
		t.Skip("requires a memcached server on localhost:11211")
	}
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
