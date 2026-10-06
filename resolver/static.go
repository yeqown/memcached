package resolver

import (
	"context"
	"strings"
	"time"

	"github.com/pkg/errors"
)

var _ Resolver = (*Static)(nil)

// Static parses comma-separated addresses without scheduling refreshes.
// Addresses may use TCP, UDP or Unix sockets; TCP is the default transport.
type Static struct{}

// NewStatic returns a resolver for TCP, UDP and Unix socket address lists.
func NewStatic() *Static { return &Static{} }

// Resolve validates and normalizes the target's addresses without DNS lookups.
// It always returns a nil next resolve time, including on error.
func (r Static) Resolve(ctx context.Context, addr string) (ResolveResult, *time.Time, error) {
	if err := ctx.Err(); err != nil {
		return ResolveResult{}, nil, err
	}
	if addr == "" {
		return ResolveResult{}, nil, errors.Wrap(ErrInvalidAddress, "empty address")
	}

	addrs := strings.Split(addr, ",")
	result := make([]*Addr, 0, len(addrs))

	for idx, address := range addrs {
		if err := ctx.Err(); err != nil {
			return ResolveResult{}, nil, err
		}
		address = strings.TrimSpace(address)
		if address == "" {
			continue
		}

		network, resolvedAddr, err := resolveAddr(address)
		if err != nil {
			return ResolveResult{}, nil, err
		}

		result = append(result, NewAddr(network, resolvedAddr, idx))
	}

	if len(result) == 0 {
		return ResolveResult{}, nil, errors.Wrap(ErrInvalidAddress, "no available address")
	}

	return ResolveResult{Addrs: result}, nil, nil
}
