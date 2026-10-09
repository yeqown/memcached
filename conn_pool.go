package memcached

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	multierror "github.com/hashicorp/go-multierror"
	"github.com/pkg/errors"
)

// The connPool holds a pool of connections to one memcached server instance
// and provides a way to get a connection from the pool.
//
// It will automatically create a new connection if the pool is empty or all
// connections are busy, of course, within the maximum connection limit. It also
// automatically removes idle connections if the idle connection count exceeds
// the maximum idle connection limit.
//
// Idle connections are connections that are not used for a certain period
// of time decided by the maxIdleTime.
//
// The pool is thread-safe.
type connPool struct {
	maxIdle, maxConns int
	maxLifeTime       time.Duration
	maxIdleTime       time.Duration

	mu         sync.Mutex // guards following
	conns      chan memcachedConn
	changed    chan struct{} // closed and replaced when waiters should retry
	createConn func(ctx context.Context) (memcachedConn, error)
	// Includes connections being dialed or closed so they retain capacity.
	numOpen atomic.Int32
	// Indicate if the pool is closed, if true, no new connections will be created
	// and all existing connections will be closed.
	closed    bool
	cleanerCh chan struct{}

	maxIdleClosed     int64 // the number of connections closed due to maxIdle
	maxIdleTimeClosed int64 // the number of connections closed due to maxIdleTime
	maxLifeTimeClosed int64 // the number of connections closed due to maxLifeTime
}

func newConnPool(
	maxIdle, maxConn int,
	maxLifeTime, maxIdleTime time.Duration,
	createConn func(ctx context.Context) (memcachedConn, error),
) *connPool {
	p := &connPool{
		maxIdle:     maxIdle,
		maxConns:    maxConn,
		maxLifeTime: maxLifeTime,
		maxIdleTime: maxIdleTime,

		mu:         sync.Mutex{},
		conns:      make(chan memcachedConn, maxConn),
		changed:    make(chan struct{}),
		createConn: createConn,
		numOpen:    atomic.Int32{},
		closed:     false,
		cleanerCh:  nil, // created when needed in startCleaner

		maxIdleClosed:     0,
		maxIdleTimeClosed: 0,
		maxLifeTimeClosed: 0,
	}

	return p
}

func (p *connPool) close() error {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil
	}

	p.closed = true
	p.notifyChangedLocked()
	// Detach idle connections under the lock; close sockets outside it.
	close(p.conns)
	if p.cleanerCh != nil {
		p.cleanerCh <- struct{}{}
	}
	idle := make([]memcachedConn, 0, len(p.conns))
	for cn := range p.conns {
		idle = append(idle, cn)
	}
	p.mu.Unlock()

	var err error
	for _, cn := range idle {
		if ce := cn.Close(); ce != nil {
			err = multierror.Append(err, errors.Wrap(ce, "close idle connection failed"))
		}
		p.releaseSlot()
	}
	return err
}

func (p *connPool) get(ctx context.Context) (memcachedConn, error) {
	for {
		p.mu.Lock()
		if p.closed {
			p.mu.Unlock()
			return nil, errors.New("connection pool is closed")
		}
		if err := ctx.Err(); err != nil {
			p.mu.Unlock()
			return nil, err
		}
		select {
		case cn := <-p.conns:
			p.mu.Unlock()
			return cn, nil
		default:
		}

		if int(p.numOpen.Load()) >= p.maxConns {
			changed := p.changed
			p.mu.Unlock()
			// Retry when a connection is returned, capacity is released, or the pool closes.
			select {
			case <-changed:
				continue
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		// Reserve capacity before dialing so concurrent dials cannot exceed the limit.
		p.numOpen.Add(1)
		p.mu.Unlock()

		cn, err := p.createConn(ctx)
		if err != nil {
			p.releaseSlot()
			return nil, err
		}
		cn.setConnPool(p)
		p.mu.Lock()
		closed := p.closed
		p.mu.Unlock()
		if closed {
			err = errors.New("connection pool is closed")
			if closeErr := cn.Close(); closeErr != nil {
				err = multierror.Append(err, closeErr)
			}
			p.releaseSlot()
			return nil, err
		}

		return cn, nil
	}
}

// notifyChangedLocked wakes all waiters to recheck state under p.mu.
func (p *connPool) notifyChangedLocked() {
	close(p.changed)
	p.changed = make(chan struct{})
}

func (p *connPool) releaseSlot() {
	p.mu.Lock()
	p.numOpen.Add(-1)
	p.notifyChangedLocked()
	p.mu.Unlock()
}

func (p *connPool) closeConn(cn memcachedConn) error {
	err := cn.Close()
	p.releaseSlot()
	return err
}

func (p *connPool) put(cn memcachedConn) error {
	if cn == nil {
		panic("pool: put nil connection")
	}

	p.mu.Lock()
	maxIdleClose := p.maxIdle > 0 && len(p.conns) >= p.maxIdle
	if p.closed || (p.maxConns > 0 && int(p.numOpen.Load()) > p.maxConns) || maxIdleClose {
		if maxIdleClose {
			p.maxIdleClosed++
		}

		p.mu.Unlock()
		return p.closeConn(cn)
	}

	select {
	case p.conns <- cn:
		p.startCleanerLocked()
		p.notifyChangedLocked()
		p.mu.Unlock()
		return nil
	default:
		p.mu.Unlock()
		// rare case, the pool is full
		return p.closeConn(cn)
	}
}

// startCleanerLocked starts a cleaner goroutine to clean up expired connections.
// NOTE: MUST run in the connPool.mu.Lock()
func (p *connPool) startCleanerLocked() {
	if (p.maxLifeTime > 0 || p.maxIdleTime > 0) && int(p.numOpen.Load()) > 0 && p.cleanerCh == nil {
		p.cleanerCh = make(chan struct{}, 1)
		go p.connectionsCleaner(p.shortestIdleTimeLocked(), p.cleanerCh)
	}
}

func (p *connPool) connectionsCleaner(d time.Duration, wake <-chan struct{}) {
	const minInterval = 1 * time.Second
	if d < minInterval {
		d = minInterval
	}

	t := time.NewTimer(d)
	defer t.Stop()

	for {
		select {
		case <-t.C:
		case <-wake: // pool was closed
		}

		p.mu.Lock()
		// This step wants to adjust the cleaner interval to the shortest idle time
		// among maxIdleTime and maxLifeTime. Avoid maxIdleTime or maxLifeTime is
		// much longer than the cleaner interval.
		d = p.shortestIdleTimeLocked()
		if p.closed || p.numOpen.Load() == 0 || d <= 0 {
			// stop the cleaner if the pool is closed or empty, or there's no need to clean up.
			p.cleanerCh = nil
			p.mu.Unlock()
			return
		}

		// make a copy of the connections those need to be closed.
		d, closing := p.connectionCleanerRunLocked(d)
		p.mu.Unlock()

		for _, cn := range closing {
			// Background cleanup has no caller to receive socket close errors.
			_ = p.closeConn(cn)
		}

		if d < minInterval {
			d = minInterval
		}

		if !t.Stop() {
			select {
			case <-t.C:
			default:
			}
		}
		t.Reset(d)
	}
}

func (p *connPool) shortestIdleTimeLocked() time.Duration {
	if p.maxIdleTime <= 0 {
		return p.maxLifeTime
	}
	if p.maxLifeTime <= 0 {
		return p.maxIdleTime
	}

	return min(p.maxLifeTime, p.maxIdleTime)
}

// connectionCleanerRunLocked will remove two class connections:
//
// 1. if the connection is expired (exceeds maxLifeTime since created).
// 2. if the connection idle time exceeds the idle connection limit(maxIdleTime).
func (p *connPool) connectionCleanerRunLocked(d time.Duration) (time.Duration, []memcachedConn) {
	var idleClosing int64
	closing := make([]memcachedConn, 0, p.maxIdle/2)
	newConns := make(chan memcachedConn, p.maxConns)

	if p.maxIdleTime > 0 {
		idleSince := nowFunc().Add(-p.maxIdleTime)
		close(p.conns)
		// TODO(@yeqown): could we optimize the loop here?
		//  we re-allocate the newConns channel every time we clean up the connections.
		for c := range p.conns {
			if d2, ok := c.idle(idleSince); !ok {
				if d2 < d {
					// Ensure idle connections are cleaned up as soon
					// as possible.
					d = d2
				}
				newConns <- c // put back
				continue
			}
			closing = append(closing, c)
			idleClosing++
		}

		p.conns = newConns
		p.maxIdleTimeClosed += idleClosing
	}

	newConns = make(chan memcachedConn, p.maxConns)
	if p.maxLifeTime > 0 {
		expiredSince := nowFunc().Add(-p.maxLifeTime)
		close(p.conns)
		for c := range p.conns {
			if d2, ok := c.expired(expiredSince); !ok {
				if d2 < d {
					// Prevents connections staying in the pool when they
					// have expired.
					d = d2
				}
				newConns <- c // put back
				continue
			}
			closing = append(closing, c)
		}

		p.conns = newConns
		p.maxLifeTimeClosed += int64(len(closing)) - idleClosing
	}

	return d, closing
}

type connPoolStats struct {
	Closed     bool
	TotalConns int
	IdleConns  int
	MaxConns   int
	MaxIdle    int

	maxIdleClosed     int64
	maxIdleTimeClosed int64
	maxLifeTimeClosed int64
}

func (p *connPool) stats() *connPoolStats {
	p.mu.Lock()
	s := &connPoolStats{
		Closed:            p.closed,
		TotalConns:        int(p.numOpen.Load()),
		IdleConns:         len(p.conns),
		MaxConns:          p.maxConns,
		MaxIdle:           p.maxIdle,
		maxIdleClosed:     p.maxIdleClosed,
		maxIdleTimeClosed: p.maxIdleTimeClosed,
		maxLifeTimeClosed: p.maxLifeTimeClosed,
	}
	p.mu.Unlock()
	return s
}
