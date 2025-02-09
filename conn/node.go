package conn

import (
	"errors"
	"sync/atomic"
	"time"
)

type Node struct {
	addr      string
	weight    int
	pool      *Pool
	available int32
}

func NewNode(addr string, weight int, maxConns int, timeout time.Duration) *Node {
	return &Node{
		addr:      addr,
		weight:    weight,
		pool:      NewPool(addr, maxConns, timeout),
		available: 1,
	}
}

func (n *Node) IsAvailable() bool {
	return atomic.LoadInt32(&n.available) == 1
}

func (n *Node) SetAvailable(available bool) {
	if available {
		atomic.StoreInt32(&n.available, 1)
	} else {
		atomic.StoreInt32(&n.available, 0)
	}
}

func (n *Node) GetConn() (*Conn, error) {
	if !n.IsAvailable() {
		return nil, errors.New("node is not available")
	}
	return n.pool.Get()
}

func (n *Node) PutConn(conn *Conn) error {
	return n.pool.Put(conn)
}
