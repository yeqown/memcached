package memcached

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/yeqown/memcached/resolver"

	"github.com/pkg/errors"
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
	dialTimeout   time.Duration
	enableSASL    bool
	plainUsername string
	plainPassword string

	addr *resolver.Addr
	pool *connPool

	state atomic.Uint32
}

func (n *node) createConn(ctx context.Context) (memcachedConn, error) {
	cn, err := newConnContext(ctx, n.addr, n.dialTimeout)
	if err != nil {
		return nil, errors.Wrap(err, "newConnContext failed")
	}

	if n.enableSASL {
		interrupted := make(chan struct{})
		stop := context.AfterFunc(ctx, func() {
			// Deadlines can interrupt auth safely; buffered conn.Close must
			// wait until authSASL has returned.
			_ = cn.raw.SetDeadline(time.Now())
			close(interrupted)
		})
		err = authSASL(cn, n.plainUsername, n.plainPassword)
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

// getConn confirms availability both before and after waiting or dialing.
// Once confirmed, the request may finish even if the node starts draining.
func (n *node) getConn(ctx context.Context) (memcachedConn, func(), error) {
	if n.status() != nodeAvailable {
		return nil, nil, ErrInstanceAbnormal
	}

	cn, err := n.pool.get(ctx)
	if err != nil {
		return nil, nil, errors.Wrap(err, "getConn failed")
	}

	if n.status() != nodeAvailable {
		_ = n.putConn(cn)
		return nil, nil, ErrInstanceAbnormal
	}

	releaseFn := sync.OnceFunc(func() {
		_ = n.putConn(cn)
	})

	return cn, releaseFn, nil
}

func (n *node) putConn(cn memcachedConn) error {
	return cn.release()
}

func (n *node) close() error {
	if n.state.CompareAndSwap(uint32(nodeAvailable), uint32(nodeDraining)) {
		return n.pool.close()
	}
	return nil
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
