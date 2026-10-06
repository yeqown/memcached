package memcached

import (
	"context"
	"sync"
	"time"

	"github.com/yeqown/memcached/resolver"

	"github.com/pkg/errors"
)

type instanceState uint8

const (
	instanceNormal instanceState = iota
	instanceDropped
)

// topologyIns coordinates one node's request lifetime and connection pool.
// addr is immutable; routing attributes come from client.active instead.
// A dropped topologyIns serves retained requests until their references reach zero.
type topologyIns struct {
	addr *resolver.Addr
	pool *connPool

	mu    sync.Mutex
	state instanceState
	refs  int
}

func (c *client) makeInstance(addr *resolver.Addr) *topologyIns {
	createConn := func(ctx context.Context) (memcachedConn, error) {
		cn, err := newConnContext(ctx, addr, c.options.dialTimeout)
		if err != nil {
			return nil, errors.Wrap(err, "newConnContext failed")
		}
		if c.options.enableSASL {
			interrupted := make(chan struct{})
			stop := context.AfterFunc(ctx, func() {
				// net.Conn deadlines are safe during I/O; conn.Close also touches
				// buffered connection state and must run after authSASL returns.
				_ = cn.raw.SetDeadline(time.Now())
				close(interrupted)
			})
			err = authSASL(cn, c.options.plainUsername, c.options.plainPassword)
			if !stop() {
				<-interrupted
			}
			if contextErr := ctx.Err(); contextErr != nil {
				err = contextErr
			}
			if err != nil {
				_ = cn.Close()
				return nil, err
			}
		}
		return cn, nil
	}
	return &topologyIns{
		addr: addr,
		pool: newConnPool(
			c.options.maxIdleConns,
			c.options.maxConns,
			c.options.maxLifetime,
			c.options.maxIdleTimeout,
			createConn,
		),
	}
}

func (i *topologyIns) acquire() error {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.state != instanceNormal {
		return ErrInstanceAbnormal
	}
	i.refs++
	return nil
}

func (i *topologyIns) getConn(ctx context.Context) (memcachedConn, error) {
	// The caller retains this instance, including while waiting or dialing.
	return i.pool.get(ctx)
}

func (i *topologyIns) release() {
	i.mu.Lock()
	i.refs--
	closing := i.state == instanceDropped && i.refs == 0
	i.mu.Unlock()
	if closing {
		// Retirement has no caller to receive errors; explicit Close reports them.
		_ = i.close()
	}
}

// drop stops new acquisitions and reports whether the pool can close now.
// It does no network I/O, so client can atomically drop and unpublish a node.
func (i *topologyIns) drop() bool {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.state = instanceDropped
	return i.refs == 0
}

func (i *topologyIns) close() error {
	err := i.pool.close()
	return err
}

func compareTopology(old, new []*resolver.Addr) (eq bool, removed []*resolver.Addr) {
	if len(old) == len(new) {
		return true, nil
	}

	// Slow path: check for removed addresses.
	nm := make(map[resolver.AddrKey]*resolver.Addr, len(new))
	for _, addr := range new {
		nm[addr.AddrKey] = addr
	}

	for _, addr := range old {
		if _, ok := nm[addr.AddrKey]; !ok {
			removed = append(removed, addr)
		}
	}

	// If no addresses were removed, and the lengths are different means no changes in the topology, so we can return true.
	if len(removed) == 0 {
		return true, nil
	}

	return false, removed
}

func (c *client) updateAddrs(resolved []*resolver.Addr) {
	// Fast path: if the topology is unchanged, no instances are added or removed.
	c.mu.RLock()
	eq, removed := compareTopology(c.active, resolved)
	c.mu.RUnlock()
	if eq {
		return
	}

	// rebuild the instance map with the new addresses,
	// retaining existing instances and creating new ones as needed.
	active := make(map[resolver.AddrKey]*topologyIns, len(resolved))
	for _, addr := range resolved {
		inst := c.instances[addr.AddrKey]
		if inst == nil {
			inst = c.makeInstance(addr)
		}

		active[addr.AddrKey] = inst
	}

	c.mu.Lock()
	// Release instances that are no longer in the topology,
	// And this should also be asynchronous as possible, so that we don't block the main thread.
	for _, addr := range removed {
		inst, ok := c.instances[addr.AddrKey]
		if !ok || inst == nil {
			continue
		}

		delete(c.instances, addr.AddrKey)

		if inst.drop() {
			_ = inst.close() // Close still attempts all idle sockets on errors.
		} else {
			// If the instance is still in use, we can close it asynchronously.
			// TODO: add a release mechanism to close the instance when it's no longer in use.
			//  inst.close()
		}
	}
	c.active, c.instances = resolved, active
	c.mu.Unlock()

	c.metrics.RecordTopology(context.Background(), len(resolved), c.generation)

	return
}

// pickInstance fixes routing and retains the selected topologyIns in one critical
// section. Updating addresses cannot drop it between selection and acquisition.
func (c *client) pickInstance(cmd, key []byte) (*topologyIns, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	addr, err := c.options.picker.Pick(c.active, cmd, key)
	if err != nil {
		return nil, errors.Wrap(err, "pick node failed")
	}
	if addr == nil {
		return nil, errors.Wrap(ErrInvalidAddress, "nil picker address")
	}

	// id := addr.AddrKey
	// id.Address, err = resolver.CanonicalAddress(id.Network, id.Address)
	// if err != nil {
	// 	return nil, err
	// }

	inst := c.instances[addr.AddrKey]
	if inst == nil {
		return nil, errors.Wrap(ErrInvalidAddress, "picker selected a node outside the active addresses")
	}

	if err = inst.acquire(); err != nil {
		return nil, errors.Wrap(err, "acquire instance failed")
	}

	return inst, nil
}

// acquireInstances retains all broadcast targets before any child starts.
func (c *client) acquireInstances() ([]*topologyIns, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	instances := make([]*topologyIns, 0, len(c.active))
	for _, addr := range c.active {
		inst := c.instances[addr.AddrKey]
		if err := inst.acquire(); err != nil {
			return nil, errors.Wrap(err, "acquire instance failed")
		}

		instances = append(instances, inst)
	}

	return instances, nil
}
