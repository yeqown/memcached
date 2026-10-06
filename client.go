package memcached

import (
	"bytes"
	"context"
	"io"
	"sync"
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
	options *clientOptions // client dynamic options to control the behavior of the client.
	target  string         // input target, which can be a comma-separated static address list or a custom resolver target.

	mu         sync.RWMutex                      // guards active addresses and topologyIns membership
	active     []*resolver.Addr                  // all active addresses, in the order they were discovered
	instances  map[resolver.AddrKey]*topologyIns // active instances, keyed by address
	generation uint64                            // topology generation, snapshot of the current topology(copy of resolver.generation)

	ctx      context.Context    // canceled when shutdown starts
	cancelFn context.CancelFunc // called to cancel ctx

	// telemetry holds the OpenTelemetry tracers and metrics.
	tracer  *telemetry.Tracer
	metrics *telemetry.Metrics
}

// New creates a Client for a comma-separated static address list or a custom
// resolver target. The resolver controls when discovery runs again; each
// attempt is bounded by WithResolveTimeout (five seconds by default).
//
// Each request retains its selected instance until its connection is returned.
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
	lifecycleCtx, lifecycleCancel := context.WithCancel(context.Background())

	c := &client{
		options: options,
		target:  addr,

		mu:         sync.RWMutex{},
		active:     make([]*resolver.Addr, 0, 2),
		instances:  make(map[resolver.AddrKey]*topologyIns),
		generation: 0,

		ctx:      lifecycleCtx,
		cancelFn: lifecycleCancel,

		tracer:  cfg.Tracer(),
		metrics: cfg.Metrics(),
	}

	next, err := c.resolve(ctx)
	if err != nil {
		lifecycleCancel()
		return nil, pkgerrors.Wrap(err, "resolve failed")
	}

	go c.resolverLoop(lifecycleCtx, next)

	return c, nil
}

func (c *client) Close() error { return c.shutdown() }

func (c *client) shutdown() (err error) {
	c.cancelFn() // Cancellation is the closed state; synchronize it with selection and updates.

	c.mu.Lock()
	count, generation := len(c.active), c.generation

	for _, inst := range c.instances {
		inst.drop()
		if err = inst.close(); err != nil {
			err = multierror.Append(err, err)
		}
	}
	c.active, c.instances = nil, nil
	c.mu.Unlock()

	c.metrics.RecordTopology(context.Background(), count, generation)

	return err
}

type callFunc func(ctx context.Context, conn memcachedConn) error

func (c *client) autoSwitchToUDP(_ context.Context, req *request, resp *response) {
	req.udpEnabled = c.options.enableUDP
	resp.udpEnabled = c.options.enableUDP
}

func (c *client) broadcastRequest(ctx context.Context, call callFunc) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	instances, err := c.acquireInstances()
	if err != nil {
		return pkgerrors.Wrap(err, "acquire instances failed")
	}
	wg := sync.WaitGroup{}

	errCh := make(chan error, len(instances))

	for _, inst := range instances {
		wg.Go(func() {
			defer inst.release()

			cn, err := inst.getConn(ctx)
			if err != nil {
				errCh <- err
				return
			}
			defer func() { _ = cn.release() }()

			if err = call(ctx, cn); err != nil {
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
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	inst, err := c.pickInstance(req.cmd, req.key)
	if err != nil {
		return err
	}
	defer inst.release()
	addr := inst.addr
	// START: Telemetry
	start := time.Now()
	var span trace.Span
	if c.tracer != nil {
		ctx, span = c.tracer.Start(ctx, string(req.cmd), addr.Address, addr.Network, string(req.key))
	}
	// END: Telemetry

	cn, err := inst.getConn(ctx)
	if err != nil {
		if c.tracer != nil {
			c.tracer.End(span, err)
		}
		if c.metrics != nil {
			c.metrics.RecordDuration(context.Background(), string(req.cmd), addr.Address, time.Since(start), err)
		}
		return pkgerrors.Wrap(err, "alloc connection failed")
	}
	defer func() { _ = cn.release() }()

	c.autoSwitchToUDP(ctx, req, resp)

	if err = req.send(ctx, cn, c.options.writeTimeout); err != nil {
		if c.tracer != nil {
			c.tracer.End(span, err)
		}
		if c.metrics != nil {
			c.metrics.RecordDuration(context.Background(), string(req.cmd), addr.Address, time.Since(start), err)
		}
		return pkgerrors.Wrap(err, "send failed")
	}

	recvErr := resp.recv(ctx, cn, c.options.readTimeout)

	// END: Telemetry
	if c.tracer != nil {
		c.tracer.End(span, recvErr)
	}
	if c.metrics != nil {
		c.metrics.RecordDuration(context.Background(), string(req.cmd), addr.Address, time.Since(start), recvErr)
	}

	return recvErr
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
