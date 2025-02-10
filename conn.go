package memcached

import (
	"bufio"
	"net"
	"time"

	"github.com/pkg/errors"
)

type Addr struct {
	Network string // Network representation of the address
	Address string // Address representation of the address

	metadata map[string]any
}

func NewAddr(network, address string) *Addr {
	return &Addr{
		Network:  network,
		Address:  address,
		metadata: make(map[string]any, 2),
	}
}

func (a *Addr) GetMetadata(mdKey string) any {
	return a.metadata[mdKey]
}

func (a *Addr) Add(mdKey string, mdValue any) {
	a.metadata[mdKey] = mdValue
}

func (a *Addr) Addr() string {
	return a.Address
}

// Conn represents a connection to a memcached server and implements the
// io.Reader/io.Writer interfaces.
type conn interface {
	Read(delim byte) (line []byte, err error)
	Write(p []byte) (n int, err error)
	Close() error
	RemoteAddr() net.Addr

	setReadTimeout(timeout time.Duration) error
	setWriteTimeout(timeout time.Duration) error
}

var (
	_ conn = (*tcpConn)(nil) // tcp socket
	// _ conn = (*unixConn)(nil) // unix domain socket
)

// tpcConn is a TCP connection
type tcpConn struct {
	raw  net.Conn
	addr net.Addr
	rr   *bufio.Reader
	wr   *bufio.Writer
}

func newConn(addr *Addr) (*tcpConn, error) {
	return newConnTimeout(addr, 3*time.Second)
}

// newConnWithContext dials a TCP connection
func newConnTimeout(addr *Addr, dialTimeout time.Duration) (*tcpConn, error) {
	rawConn, err := net.DialTimeout("tcp", addr.Addr(), dialTimeout)
	if err != nil {
		return nil, errors.Wrap(err, "dialContext")
	}

	conn := &tcpConn{
		raw:  rawConn,
		addr: rawConn.RemoteAddr(),
		rr:   bufio.NewReader(rawConn),
		wr:   bufio.NewWriter(rawConn),
	}

	return conn, nil
}

func (c *tcpConn) setReadTimeout(timeout time.Duration) error {
	return c.raw.SetReadDeadline(time.Now().Add(timeout))
}

func (c *tcpConn) setWriteTimeout(timeout time.Duration) error {
	return c.raw.SetWriteDeadline(time.Now().Add(timeout))
}

// Read reads data from the connection
func (c *tcpConn) Read(delim byte) (line []byte, err error) {
	return c.rr.ReadSlice(delim)
}

// Write writes data to the connection
func (c *tcpConn) Write(p []byte) (n int, err error) {
	n, err = c.wr.Write(p)
	if err != nil {
		return n, err
	}
	return n, c.wr.Flush()
}

// Close closes the connection
func (c *tcpConn) Close() error {
	return c.raw.Close()
}

// RemoteAddr returns the remote network address
func (c *tcpConn) RemoteAddr() net.Addr {
	return c.addr
}

// TODO: conn pool which allows the client can hold multiple connections
// to serve multiple requests at the same time.
type connPool struct {
	maxIdle, maxConns int
	idleTimeout       time.Duration
	conns             chan conn
}

func newConnPool(maxIdle, maxConn int, idleTimeout time.Duration) *connPool {
	return &connPool{}
}
