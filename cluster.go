package memcached

import (
	"math/rand"
	"strings"
	"time"

	"github.com/pkg/errors"
)

var (
	ErrInvalidAddress = errors.New("invalid address")
)

// Resolver is responsible for resolving a given address
// to a list of Addr, and also support custom address format.
//
// TODO: resolver should be periodically refreshed, so that we can
//  eliminate the dead Addr from the cluster. But this is an optional feature.
type Resolver interface {
	Resolve(addr string) ([]*Addr, error)
}

// Picker is responsible for picking a given key to a specific Addr
// while considering the cluster state.
type Picker interface {
	Pick(cmd, key string) (*Addr, error)
}

// Builder is responsible for building a Picker from a given list of Addr.
type Builder interface {
	Build(addrs []*Addr) Picker
}

// The defaultResolver is the default implementation of Resolver.
// It will resolve the given address to a list of Addr.
// For example, if the given address is "localhost:11211",
// it will return a list of Addr with only one Addr.
// If the given address is "localhost:11211,localhost:11212",
// it will return a list of Addr with two Addr.
type defaultResolver struct{}

func (r defaultResolver) Resolve(addr string) ([]*Addr, error) {
	if addr == "" {
		return nil, errors.Wrap(ErrInvalidAddress, "empty address")
	}

	addrs := strings.Split(addr, ",")
	result := make([]*Addr, 0, len(addrs))

	for _, address := range addrs {
		address = strings.TrimSpace(address)
		if address == "" {
			continue
		}

		result = append(result, NewAddr("tcp", address))
	}

	if len(result) == 0 {
		return nil, errors.Wrap(ErrInvalidAddress, "no available address")
	}

	return result, nil
}

// The randomPicker is the default implementation of Picker.
// It will pick a given key to a random Addr.
type randomPicker struct {
	r         *rand.Rand
	addrs     []*Addr
	addrCount int
}

func (p *randomPicker) Pick(_, _ string) (*Addr, error) {
	if p.addrCount == 0 {
		return nil, errors.Wrap(ErrInvalidAddress, "no available address")
	}
	if p.addrCount == 1 {
		return p.addrs[0], nil
	}

	return p.addrs[p.r.Intn(p.addrCount)], nil
}

type randomPickBuilder struct{}

func (b randomPickBuilder) Build(addrs []*Addr) Picker {
	return &randomPicker{
		r:         rand.New(rand.NewSource(time.Now().UnixNano())),
		addrs:     addrs,
		addrCount: len(addrs),
	}
}
