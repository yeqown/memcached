package conn

import (
	"errors"
	"sync"
	"time"
)

type Pool struct {
	mu       sync.Mutex
	addr     string
	maxConns int
	timeout  time.Duration
	conns    chan *Conn
}

func NewPool(addr string, maxConns int, timeout time.Duration) *Pool {
	return &Pool{
		addr:     addr,
		maxConns: maxConns,
		timeout:  timeout,
		conns:    make(chan *Conn, maxConns),
	}
}

func (p *Pool) Get() (*Conn, error) {
	select {
	case conn := <-p.conns:
		return conn, nil
	default:
		return NewConn(p.addr, p.timeout)
	}
}

func (p *Pool) Put(conn *Conn) error {
	if conn == nil {
		return errors.New("connection is nil")
	}

	select {
	case p.conns <- conn:
		return nil
	default:
		return conn.Close()
	}
}
