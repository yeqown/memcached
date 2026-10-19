package memcached

import (
	"bytes"
	"context"
	"io"
	"sync"
	"sync/atomic"
	"time"

	multierror "github.com/hashicorp/go-multierror"
	pkgerrors "github.com/pkg/errors"
	"go.opentelemetry.io/otel/trace"

	"github.com/yeqown/memcached/resolver"
	"github.com/yeqown/memcached/telemetry"
)

// Client represents a memcached client API set.
type Client interface {
	io.Closer

	basicTextProtocolCommander
	metaTextProtocolCommander
	statisticsTextProtocolCommander
	// TODO: support rawTextProtocolCommander
	// rawTextProtocolCommander
}

var _ Client = (*client)(nil)

type client struct {
	options *clientOptions

	topology *topology // owns discovery, membership and node lifetimes

	// telemetry holds the OpenTelemetry tracers and metrics.
	tracer  *telemetry.Tracer
	metrics *telemetry.Metrics
}

// New creates a Client for a comma-separated static address list or a custom
// resolver target. The resolver controls when discovery runs again; each
// attempt is bounded by WithResolveTimeout (five seconds by default).
//
// Borrowed connections may finish their requests after their node is removed.
// Gets and GetAndTouches send all requested keys to the node selected by the
// first key; callers must ensure those keys reside together.
func New(addr string, opts ...ClientOption) (Client, error) {
	return NewWithContext(context.Background(), addr, opts...)
}

// NewWithContext creates a Client with target address and context.
// The context is used for the initial resolve ONLY.
func NewWithContext(ctx context.Context, addr string, opts ...ClientOption) (Client, error) {
	options := newClientOptions()
	for _, opt := range opts {
		opt(options)
	}

	cfg := telemetry.NewConfig(options.telemetryOptions...)
	metrics := cfg.Metrics()
	makeNode := func(addr *resolver.Addr) *node {
		n := &node{
			addr: addr.Clone(),
			pool: newConnPool(
				options.maxIdleConns,
				options.maxConns,
				options.maxLifetime,
				options.maxIdleTimeout,
				createConnFactory(addr, options.dialTimeout, options.enableSASL, options.plainUsername, options.plainPassword),
			),
			state: atomic.Uint32{},
		}
		n.state.Store(uint32(nodeAvailable))

		return n
	}

	topology, err := newTopology(ctx, addr, options.resolver, options.resolveTimeout, makeNode, metrics)
	if err != nil {
		return nil, pkgerrors.Wrap(err, "newTopology failed")
	}

	return &client{
		options:  options,
		topology: topology,
		tracer:   cfg.Tracer(),
		metrics:  metrics,
	}, nil
}

func (c *client) Close() error { return c.topology.close() }

// pickConn routes and borrows a connection using the supplied request context.
// It returns the selected address even if borrowing fails.
// The caller supplies a fresh handle whose release method is idempotent.
func (c *client) pickConn(ctx context.Context, cmd, key []byte, borrowed *borrowedConn) (*resolver.Addr, error) {
	n, err := c.topology.pickNode(c.options.picker, cmd, key)
	if err != nil {
		return nil, pkgerrors.Wrap(err, "pick node failed")
	}

	if err := n.getConn(ctx, borrowed); err != nil {
		return n.addr, pkgerrors.Wrap(err, "alloc connection failed")
	}

	return n.addr, nil
}

type callFunc func(ctx context.Context, conn memcachedConn) error

func switchToUDP(req *request, resp *response, enabled bool) {
	req.udpEnabled = enabled
	resp.udpEnabled = enabled
}

func (c *client) broadcastRequest(ctx context.Context, call callFunc, cmd string) error {
	var start time.Time
	if c.metrics != nil {
		start = time.Now()
	}
	var span trace.Span
	if c.tracer != nil {
		ctx, span = c.tracer.Start(ctx, "broadcast:"+cmd, "", "", "")
	}

	finish := func(err error) {
		if c.tracer != nil {
			c.tracer.End(span, err)
		}
		if c.metrics != nil {
			c.metrics.RecordDuration(context.Background(), cmd, "", time.Since(start), err)
		}
	}

	nodes, err := c.topology.allNodes()
	if err != nil {
		finish(err)
		return pkgerrors.Wrap(err, "broadcastRequest fetch nodes failed")
	}

	var (
		wg    sync.WaitGroup
		errCh = make(chan error, len(nodes))
	)

	for _, n := range nodes {
		wg.Go(func() {
			var borrowed borrowedConn
			if err := n.getConn(ctx, &borrowed); err != nil {
				errCh <- err
				return
			}
			defer borrowed.release()
			if err := call(ctx, borrowed.conn); err != nil {
				errCh <- err
			}
		})
	}
	wg.Wait()
	close(errCh)

	var multiErr error
	for err := range errCh {
		multiErr = multierror.Append(multiErr, err)
	}

	finish(multiErr)
	return multiErr
}

func (c *client) dispatchRequest(ctx context.Context, req *request, resp *response) error {
	var start time.Time
	if c.metrics != nil {
		start = time.Now()
	}
	var operation string
	if c.tracer != nil || c.metrics != nil {
		operation = string(req.cmd)
	}
	var span trace.Span
	if c.tracer != nil {
		ctx, span = c.tracer.Start(ctx, operation, "", "", string(req.key))
	}
	var server string

	finish := func(err error) {
		if c.tracer != nil {
			c.tracer.End(span, err)
		}
		if c.metrics != nil {
			c.metrics.RecordDuration(context.Background(), operation, server, time.Since(start), err)
		}
	}

	var borrowed borrowedConn
	addr, err := c.pickConn(ctx, req.cmd, req.key, &borrowed)
	if addr != nil {
		server = addr.Address
		if c.tracer != nil {
			c.tracer.SetServer(span, server, addr.Network)
		}
	}
	if err != nil {
		finish(err)
		return pkgerrors.Wrap(err, "pickConn failed")
	}
	defer borrowed.release()

	switchToUDP(req, resp, c.options.enableUDP)

	if err = req.send(ctx, borrowed.conn, c.options.writeTimeout); err != nil {
		finish(err)
		return pkgerrors.Wrap(err, "send failed")
	}

	err = resp.recv(ctx, borrowed.conn, c.options.readTimeout)
	finish(err)
	return err
}

// authSASL performs the Binary SASL authentication.
// https://docs.memcached.org/protocols/binarysasl/
// https://datatracker.ietf.org/doc/html/rfc4422
//
// https://en.wikipedia.org/wiki/Simple_Authentication_and_Security_Layer
// SASL mechanism:
// EXTERNAL, ANONYMOUS, PLAIN, OTP, SKEY, CRAM-MD5, DIGEST-MD5, SCRAM, NTLM, GS2-, GSSAPI and more.
//
// But here we only support a PLAIN mechanism for now.
// https://datatracker.ietf.org/doc/html/rfc4616
func authSASL(conn memcachedConn, username, password string) error {
	// 1. first, list mechanisms the server supports
	req, resp := saslListMechanisms()
	if err := req.send(conn); err != nil {
		return pkgerrors.Wrap(err, "authSASL send")
	}
	if err := resp.read(conn); err != nil {
		return pkgerrors.Wrap(err, "authSASL recv")
	}
	if err := resp.expect(_binaryStatusOK); err != nil {
		return pkgerrors.Wrap(err, "authSASL")
	}

	if !bytes.Contains(resp.value, []byte("PLAIN")) {
		return pkgerrors.New("memcached server does not support PLAIN mechanism")
	}

	// 2. choose one mechanism and send the authentication request
	req, resp = saslAuthRequestPlain(username, password)
	if err := req.send(conn); err != nil {
		return pkgerrors.Wrap(err, "authSASL send")
	}
	if err := resp.read(conn); err != nil {
		return pkgerrors.Wrap(err, "authSASL recv")
	}
	if err := resp.expect(_binaryStatusOK); err != nil {
		return pkgerrors.Wrap(err, "authSASL")
	}

	return nil
}
