package memcached

import (
	"context"
	"sync"
	"time"
	"bytes"
	"io"

	"github.com/pkg/errors"
)

// Item represents a key-value pair to be got or stored.
type Item struct {
	Key   string
	Value []byte
	Flags uint32
}

type normalTextProtocolClient interface {
	Set(key, value string, flags, expiry uint32) error
	Touch(key string, expiry uint32) error
	Get(key string) (*Item, error)
	Gets(keys ...string) ([]*Item, error)
}

type metaTextProtocolClient interface {
	MetaSet(key string)
	MetaGet(key string)
}

// Client represents a memcached client API set.
type Client interface {
	io.Closer
	normalTextProtocolClient
	// metaTextProtocolClient

	Version() (string, error)
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

	// conns represents the all connections to the memcached server instances.
	// TODO(@yeqown): using connection pool instead of a map to store the connections.
	connsLock sync.RWMutex
	conns     map[*Addr]conn
}

func New(addr string, opts ...ClientOption) (Client, error) {
	timeoutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return newClientWithContext(timeoutCtx, addr, opts...)
}

func newClientWithContext(ctx context.Context, addr string, opts ...ClientOption) (Client, error) {
	options := newClientOptions()
	for _, opt := range opts {
		opt(options)
	}

	addrs, err := options.resolver.Resolve(addr)
	if err != nil {
		return nil, errors.Wrap(err, "failed to resolve address")
	}

	if len(addrs) == 0 {
		return nil, errors.Wrap(ErrInvalidAddress, "empty address")
	}
	picker := options.pickBuilder.Build(addrs)

	return &client{
		options: options,
		addrs:   addrs,
		picker:  picker,

		connsLock: sync.RWMutex{},
		conns:     make(map[*Addr]conn, 4),
	}, nil
}

func (c *client) Close() error {
	c.connsLock.Lock()
	defer c.connsLock.Unlock()

	for _, conn := range c.conns {
		if err := conn.Close(); err != nil {
			return errors.Wrap(err, "failed to close connection")
		}
	}

	return nil
}

func (c *client) pickConn(cmd, key string) (conn, error) {
	addr, err := c.picker.Pick(c.addrs, cmd, key)
	if err != nil {
		return nil, errors.Wrap(err, "failed to pick a connection")
	}

	conn, err := c.getConn(addr)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get a connection")
	}

	return conn, nil
}

// getConn returns a connection to the given address.
// TODO(@yeqown): support connection pool
func (c *client) getConn(addr *Addr) (conn, error) {
	c.connsLock.RLock()
	conn, ok := c.conns[addr]
	if ok {
		c.connsLock.RUnlock()
		return conn, nil
	}
	c.connsLock.RUnlock()

	// otherwise, create a new connection
	conn, err := newConn(addr)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create a new connection")
	}

	// add to the map
	c.connsLock.Lock()
	c.conns[addr] = conn
	c.connsLock.Unlock()

	return conn, nil
}

func (c *client) doRequest(req *request, resp *response) error {
	conn, err := c.pickConn("version", "")
	if err != nil {
		return errors.Wrap(err, "failed to pick a connection")
	}

	_ = conn.setReadTimeout(c.options.readTimeout)
	if err = req.send(conn); err != nil {
		return errors.Wrap(err, "failed to write command")
	}

	_ = conn.setWriteTimeout(c.options.writeTimeout)
	return resp.recv(conn)
}

func (c *client) Version() (string, error) {
	req := buildVersionCommand()
	resp := buildResponse1(1)
	if err := c.doRequest(req, resp); err != nil {
		return "", errors.Wrap(err, "do request")
	}

	// parse version number from response
	// VERSION 1.6.14
	if !bytes.HasPrefix(resp.raw, _VersionBytes) {
		return "", errors.Wrap(ErrMalformedResponse, string(resp.raw))
	}

	return string(cleanLine(resp.raw[8:])), nil
}

func (c *client) Set(key, value string, flags, expiry uint32) error {
	req := buildStorageCommand("set", key, []byte(value), 0, expiry, false)
	resp := buildResponse1(1)
	if err := c.doRequest(req, resp); err != nil {
		return errors.Wrap(err, "do request")
	}

	if resp.err != nil {
		return resp.err
	}

	// No error encountered, expect STORED\r\n
	if !bytes.Equal(resp.raw, _StoredCRLFBytes) {
		return errors.Wrap(ErrMalformedResponse, string(resp.raw))
	}

	return nil
}

func (c *client) Touch(key string, expiry uint32) error {
	req := buildTouchCommand(key, expiry, true)
	resp := buildResponse1(1)
	if err := c.doRequest(req, resp); err != nil {
		return errors.Wrap(err, "do request")
	}

	return resp.err
}

// Get gets the value of the given key.
func (c *client) Get(key string) (*Item, error) {
	req := buildGetCommand(key)
	resp := buildResponse1(3)
	if err := c.doRequest(req, resp); err != nil {
		return nil, errors.Wrap(err, "do request")
	}

	// parse response
	items, err := parseItems(resp.raw)
	if err != nil {
		return nil, errors.Wrap(ErrMalformedResponse, err.Error())
	}
	if len(items) == 0 {
		return nil, errors.Wrap(ErrNotFound, "no items found")
	}

	return items[0], nil
}

// Gets the values of the given keys.
//
// BUT you must know that the cluster mode of memcached DOES NOT support this command,
// since keys are possible stored in different memcached instances.
// Be careful when using this command unless you are sure that
// all keys are stored in the same memcached instance.
func (c *client) Gets(keys ...string) ([]*Item, error) {
	req := buildGetsCommand(keys...)
	resp := buildResponse2(_EndCRLFBytes)
	if err := c.doRequest(req, resp); err != nil {
		return nil, errors.Wrap(err, "do request")
	}

	// parse response
	items, err := parseItems(resp.raw)
	if err != nil {
		return nil, errors.Wrap(ErrMalformedResponse, err.Error())
	}
	if len(items) == 0 {
		return nil, errors.Wrap(ErrNotFound, "no items found")
	}

	return items, nil
}
