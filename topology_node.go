package memcached

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pkg/errors"

	"github.com/yeqown/memcached/resolver"
)

type nodeState uint32

const (
	nodeAvailable nodeState = iota
	nodeDraining
	nodeClosed
)

// node owns an immutable identity, connection configuration and lifecycle.
// Pool owns connection resources and counters; all borrowing/returning goes
// through node. Routing attributes come from topology's cached addresses.
type node struct {
	addr *resolver.Addr
	pool *connPool

	state atomic.Uint32
}

// borrowedConn is a single-use handle owned by one request. It must not be
// copied or reused; concurrent release calls wait for the first cleanup.
type borrowedConn struct {
	node *node
	conn memcachedConn
	once sync.Once
}

func (b *borrowedConn) release() {
	b.once.Do(func() {
		_ = b.node.putConn(b.conn)
	})
}

type createConnFunc func(ctx context.Context) (memcachedConn, error)

func createConnFactory(
	addr *resolver.Addr,
	dialTimeout time.Duration,
	enableSASL bool,
	plainUsername, plainPassword string,
) createConnFunc {
	return func(ctx context.Context) (memcachedConn, error) {
		cn, err := newConnContext(ctx, addr, dialTimeout)
		if err != nil {
			return nil, errors.Wrap(err, "newConnContext failed")
		}

		if enableSASL {
			interrupted := make(chan struct{})
			stop := context.AfterFunc(ctx, func() {
				// Deadlines can interrupt auth safely; buffered conn.Close must
				// wait until authSASL has returned.
				_ = cn.raw.SetDeadline(time.Now())
				close(interrupted)
			})
			err = authSASL(cn, plainUsername, plainPassword)
			if !stop() {
				<-interrupted
			}
			if contextErr := ctx.Err(); contextErr != nil {
				err = contextErr
			}
			if err != nil {
				_ = cn.Close() // Preserve the authentication error.
				return nil, err
			}
		}

		return cn, nil
	}
}

// getConn confirms availability both before and after waiting or dialing.
// Once confirmed, the request may finish even if the node starts draining.
func (n *node) getConn(ctx context.Context, borrowed *borrowedConn) error {
	if n.status() != nodeAvailable {
		return ErrInstanceAbnormal
	}

	cn, err := n.pool.get(ctx)
	if err != nil {
		return errors.Wrap(err, "getConn failed")
	}

	if n.status() != nodeAvailable {
		_ = n.putConn(cn)
		return ErrInstanceAbnormal
	}

	borrowed.node, borrowed.conn = n, cn
	return nil
}

func (n *node) putConn(cn memcachedConn) error {
	return cn.release()
}

// drain rejects new borrows without waiting for connection cleanup.
func (n *node) drain() {
	n.state.CompareAndSwap(uint32(nodeAvailable), uint32(nodeDraining))
}

func (n *node) close() error {
	n.drain()
	return n.pool.close()
}

func (n *node) status() nodeState {
	state := n.state.Load()
	if state == uint32(nodeDraining) {
		stats := n.pool.stats()
		if stats.Closed && stats.TotalConns == 0 {
			n.state.Store(uint32(nodeClosed))
		}
	}

	return nodeState(n.state.Load())
}
