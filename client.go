package memcached

import (
	"context"
	"io"
	"sync"
	"time"

	"github.com/pkg/errors"
)

// Client represents a memcached client API set.
type Client interface {
	io.Closer
	basicTextProtocolCommander
	// metaTextProtocolCommander

	Version(ctx context.Context) (string, error)
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

func (c *client) pickConn(ctx context.Context, cmd, key string) (memcachedConn, error) {
	addr, err := c.picker.Pick(c.addrs, cmd, key)
	if err != nil {
		return nil, errors.Wrap(err, "pick node failed")
	}

	cn, err := c.allocConn(ctx, addr)
	if err != nil {
		return nil, errors.Wrap(err, "alloc connection failed")
	}

	return cn, nil
}

// allocConn returns a true connection from the pool.
func (c *client) allocConn(ctx context.Context, addr *Addr) (memcachedConn, error) {
	c.mu.Lock()
	pool, ok := c.connPools[addr]
	if ok {
		c.mu.Unlock()
		return pool.get(ctx)
	}

	wrapNewConn := func(ctx2 context.Context) (memcachedConn, error) {
		return newConnContext(ctx2, addr, c.options.dialTimeout)
	}

	// could not find pool for the given addr, create a new one
	pool = newConnPool(
		c.options.maxIdleConns, c.options.maxConns,
		c.options.maxLifetime, c.options.maxIdleTimeout,
		wrapNewConn,
	)
	c.connPools[addr] = pool
	c.mu.Unlock()

	return pool.get(ctx)
}

func (c *client) doRequest(ctx context.Context, req *request, resp *response) error {
	cn, err := c.pickConn(ctx, "version", "")
	if err != nil {
		return errors.Wrap(err, "pickConn failed")
	}

	_ = cn.setReadTimeout(c.options.readTimeout)
	if err = req.send(cn); err != nil {
		return errors.Wrap(err, "send failed")
	}

	_ = cn.setWriteTimeout(c.options.writeTimeout)
	return resp.recv(cn)
}
