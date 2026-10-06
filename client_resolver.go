package memcached

import (
	"context"
	"log"
	"time"
)

// resolve is serial within a Client and is shared by startup and background
// discovery. Copy the schedule before applying so Resolver owns no published data.
func (c *client) resolve(ctx context.Context) (*time.Time, error) {
	start := time.Now()

	resolveCtx, cancel := context.WithTimeout(ctx, c.options.resolveTimeout)
	defer cancel()

	result, nextAt, err := c.options.resolver.Resolve(resolveCtx, c.target)
	if err != nil {
		return nextAt, err
	}

	// If the generation has changed, update the addresses.
	// This is done asynchronously to avoid blocking the resolver loop, but we still respect the next resolve time.
	if c.generation != result.Generation {
		c.updateAddrs(result.Addrs)
	}

	if c.metrics != nil {
		c.metrics.RecordResolve(ctx, time.Since(start), err)
	}

	return nextAt, err
}

func (c *client) resolverLoop(ctx context.Context, nextAt *time.Time) {
	if nextAt == nil {
		// No next resolve time means no refreshes, including on error.
		return
	}

	var err error

	for {
		timer := time.NewTimer(max(0, time.Until(*nextAt)))

		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}

		if ctx.Err() != nil {
			log.Printf("resolverLoop: context quit: %v\n", ctx.Err())
			return
		}

		// The Resolver's schedule remains authoritative even when resolve fails.
		if nextAt, err = c.resolve(ctx); err != nil {
			log.Printf("resolverLoop: resolve failed: %v\n", err)
		}
		if nextAt == nil {
			log.Printf("resolverLoop: resolver returned nil next resolve time, stopping refreshes\n")
			return
		}
	}
}
