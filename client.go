package memcached

import (
	"bytes"
	"context"
	"io"
	"sync"
	"time"

	multierror "github.com/hashicorp/go-multierror"
	"github.com/pkg/errors"
)

// Client represents a memcached client API set.
type Client interface {
	io.Closer

	basicTextProtocolCommander
	metaTextProtocolCommander
}

var (
	_ Client = (*client)(nil)
)

type client struct {
	options *clientOptions

	// addrs represents the list of memcached addresses.
	// Each one of them means a memcached server instance.
	addrs []*Addr

	// picker represents the picker strategy.
	// It is used to pick a memcached server instance to execute a command.
	picker Picker

	mu        sync.Mutex // guards following
	connPools map[*Addr]*connPool
}

// New creates a new memcached client with the given address and options.
//
// The client contains a connection pool to manage the connections to
// one memcached instance. And it can manage multiple memcached instances with
// cluster mode.
//
// The Cluster mode means that the client can connect to multiple memcached instances
// and automatically pick a memcached instance to execute a command, of course,
// the client make sure that the same key will be executed on the same memcached instance.
// Be careful, there are some `keys` command does not obey this rule, such as `gets`, `gats`.
func New(addr string, opts ...ClientOption) (Client, error) {
	timeoutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return newClientWithContext(timeoutCtx, addr, opts...)
}

func newClientWithContext(_ context.Context, addr string, opts ...ClientOption) (Client, error) {
	options := newClientOptions()
	for _, opt := range opts {
		opt(options)
	}

	addrs, err := options.resolver.Resolve(addr)
	if err != nil {
		return nil, errors.Wrap(err, "resolve failed")
	}

	if len(addrs) == 0 {
		return nil, errors.Wrap(ErrInvalidAddress, "empty address")
	}
	picker := options.pickBuilder.Build(addrs)

	return &client{
		options: options,
		addrs:   addrs,
		picker:  picker,

		mu:        sync.Mutex{},
		connPools: make(map[*Addr]*connPool, 4),
	}, nil
}

func (c *client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	for _, pool := range c.connPools {
		if err := pool.close(); err != nil {
			return errors.Wrap(err, "Close")
		}
	}

	return nil
}

type releaseConnFn func(memcachedConn) error

// getConn returns a true connection from the pool.
func (c *client) getConn(ctx context.Context, addr *Addr) (memcachedConn, releaseConnFn, error) {
	c.mu.Lock()
	pool, ok := c.connPools[addr]
	if ok {
		c.mu.Unlock()
		cn, err := pool.get(ctx)
		return cn, pool.put, err
	}

	wrapNewConn := func(ctx2 context.Context) (cn memcachedConn, err error) {
		switch addr.Network {
		case "tcp", "tcp4", "tcp6":
			cn, err = newConnContext(ctx2, addr, c.options.dialTimeout)
		default:
			// TODO(@yeqown): "udp", "unix" not supported yet
			panic("not supported yet")
		}
		if err != nil {
			return nil, errors.Wrap(err, "newConnContext failed")
		}

		// SASL auth if enabled
		if c.options.enableSASL {
			if err = authSASL(cn, c.options.plainUsername, c.options.plainPassword); err != nil {
				_ = cn.Close()
				return nil, err
			}
		}

		return cn, nil
	}

	// could not find pool for the given addr, create a new one
	pool = newConnPool(
		c.options.maxIdleConns, c.options.maxConns,
		c.options.maxLifetime, c.options.maxIdleTimeout,
		wrapNewConn,
	)
	c.connPools[addr] = pool
	c.mu.Unlock()

	cn, err := pool.get(ctx)
	return cn, pool.put, err
}

func (c *client) broadcastRequest(ctx context.Context, req *request, resp *response) error {
	wg := sync.WaitGroup{}

	execute := func(cn memcachedConn) error {
		_ = cn.setReadTimeout(c.options.readTimeout)
		if err := req.send(cn); err != nil {
			return errors.Wrap(err, "send failed")
		}

		_ = cn.setWriteTimeout(c.options.writeTimeout)
		return resp.recv(cn)
	}
	errCh := make(chan error, len(c.addrs))

	for _, addr := range c.addrs {
		wg.Add(1)
		addrCopy := addr
		go func() {
			defer wg.Done()

			cn, returnToPool, err := c.getConn(ctx, addrCopy)
			if err != nil {
				errCh <- err
				return
			}
			defer func() { _ = returnToPool(cn) }()

			if err = execute(cn); err != nil {
				errCh <- err
			}
		}()
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
	addr, err := c.picker.Pick(c.addrs, req.cmd, req.key)
	if err != nil {
		return errors.Wrap(err, "pick node failed")
	}

	cn, returnToPool, err := c.getConn(ctx, addr)
	if err != nil {
		return errors.Wrap(err, "alloc connection failed")
	}
	defer func() { _ = returnToPool(cn) }()

	_ = cn.setReadTimeout(c.options.readTimeout)
	if err = req.send(cn); err != nil {
		return errors.Wrap(err, "send failed")
	}

	_ = cn.setWriteTimeout(c.options.writeTimeout)
	return resp.recv(cn)
}

// authSASL performs the Binary SASL authentication.
// https://docs.memcached.org/protocols/binarysasl/
// https://datatracker.ietf.org/doc/html/rfc4422
//
// https://en.wikipedia.org/wiki/Simple_Authentication_and_Security_Layer
// SASL mechanism:
// EXTERNAL, ANONYMOUS, PLAIN, OTP, SKEY, CRAM-MD5, DIGEST-MD5, SCRAM, NTLM, GS2-, GSSAPI and more.
//
// But here we only support PLAIN mechanism for now.
// https://datatracker.ietf.org/doc/html/rfc4616
func authSASL(conn memcachedConn, username, password string) error {
	// 1. first of all, list mechanisms the server supports
	req, resp := saslListMechanisms()
	if err := req.send(conn); err != nil {
		return errors.Wrap(err, "authSASL send")
	}
	if err := resp.read(conn); err != nil {
		return errors.Wrap(err, "authSASL recv")
	}
	if err := resp.expect(_binaryStatusOK); err != nil {
		return errors.Wrap(err, "authSASL")
	}

	if !bytes.Contains(resp.value, []byte("PLAIN")) {
		return errors.New("memcached server does not support PLAIN mechanism")
	}

	// 2. choose one mechanism and send the authentication request
	req, resp = saslAuthRequestPlain(username, password)
	if err := req.send(conn); err != nil {
		return errors.Wrap(err, "authSASL send")
	}
	if err := resp.read(conn); err != nil {
		return errors.Wrap(err, "authSASL recv")
	}
	if err := resp.expect(_binaryStatusOK); err != nil {
		return errors.Wrap(err, "authSASL")
	}

	return nil
}
