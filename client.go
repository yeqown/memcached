package memcached

import (
	"bytes"
	"context"
	"io"
	"sync"
	"time"

	multierror "github.com/hashicorp/go-multierror"
	pkgerrors "github.com/pkg/errors"

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
	makeNode := func(addr *resolver.Addr) *node {
		n := &node{
			addr:          addr.Clone(),
			dialTimeout:   options.dialTimeout,
			enableSASL:    options.enableSASL,
			plainUsername: options.plainUsername,
			plainPassword: options.plainPassword,
		}

		n.pool = newConnPool(
			options.maxIdleConns,
			options.maxConns,
			options.maxLifetime,
			options.maxIdleTimeout,
			n.createConn,
		)

		return n
	}

	topology, err := newTopology(ctx, addr, options.resolver, options.resolveTimeout, makeNode, cfg.Metrics())
	if err != nil {
		return nil, pkgerrors.Wrap(err, "newTopology failed")
	}

	return &client{
		options:  options,
		topology: topology,
		tracer:   cfg.Tracer(),
		metrics:  cfg.Metrics(),
	}, nil
}

func (c *client) Close() error { return c.topology.close() }

// pickConn keeps routing, borrowing and request cleanup together. The returned
// context contains the request span; releaseFn records the result and releases
// the connection through node. It is safe to call more than once.
func (c *client) pickConn(ctx context.Context, cmd, key []byte) (context.Context, memcachedConn, func(error), error) {
	n, err := c.topology.pickNode(c.options.picker, cmd, key)
	if err != nil {
		return ctx, nil, nil, err
	}
	addr := n.addr
	start := time.Now()
	ctx, span := c.tracer.Start(ctx, string(cmd), addr.Address, addr.Network, string(key))

	finish := func(err error) {
		c.tracer.End(span, err)
		c.metrics.RecordDuration(context.Background(), string(cmd), addr.Address, time.Since(start), err)
	}
	cn, releaseConn, err := n.getConn(ctx)
	if err != nil {
		finish(err)
		return ctx, nil, nil, pkgerrors.Wrap(err, "alloc connection failed")
	}
	var once sync.Once
	releaseFn := func(err error) {
		once.Do(func() {
			finish(err)
			releaseConn()
		})
	}
	return ctx, cn, releaseFn, nil
}

type callFunc func(ctx context.Context, conn memcachedConn) error

func switchToUDP(req *request, resp *response, enabled bool) {
	req.udpEnabled = enabled
	resp.udpEnabled = enabled
}

func (c *client) broadcastRequest(ctx context.Context, call callFunc) error {
	nodes, err := c.topology.allNodes()
	if err != nil {
		return err
	}
	var wg sync.WaitGroup
	errCh := make(chan error, len(nodes))
	for _, n := range nodes {
		wg.Go(func() {
			cn, releaseFn, err := n.getConn(ctx)
			if err != nil {
				errCh <- err
				return
			}
			defer releaseFn()
			if err := call(ctx, cn); err != nil {
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
	return multiErr
}

func (c *client) dispatchRequest(ctx context.Context, req *request, resp *response) error {
	connCtx, cn, releaseFn, err := c.pickConn(ctx, req.cmd, req.key)
	if err != nil {
		return pkgerrors.Wrap(err, "pickConn failed")
	}
	defer func() { releaseFn(err) }()

	switchToUDP(req, resp, c.options.enableUDP)

	if err = req.send(connCtx, cn, c.options.writeTimeout); err != nil {
		return pkgerrors.Wrap(err, "send failed")
	}

	err = resp.recv(connCtx, cn, c.options.readTimeout)
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
