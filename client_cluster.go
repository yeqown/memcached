package memcached

import (
	"hash/crc32"
	"net"
	"strings"

	"github.com/pkg/errors"

	"github.com/yeqown/memcached/hash"
)

// Resolver is responsible for resolving a given address
// to a list of Addr, and also support custom address format.
//
// TODO: resolver should be periodically refreshed, so that we can
// eliminate the dead Addr from the cluster. But this is an optional feature.
type Resolver interface {
	Resolve(addr string) ([]*Addr, error)
}

// Picker is responsible for picking a given key to a specific Addr
// while considering the cluster state.
type Picker interface {
	Pick(addr []*Addr, cmd, key []byte) (*Addr, error)
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

	for idx, address := range addrs {
		address = strings.TrimSpace(address)
		if address == "" {
			continue
		}

		// TODO: support udp and unix socket address format.
		v, err := net.ResolveTCPAddr("tcp", address)
		if err != nil {
			return nil, errors.Wrap(err, "invalid address: "+address)
		}

		result = append(result, NewAddr(v.Network(), address, idx))
	}

	if len(result) == 0 {
		return nil, errors.Wrap(ErrInvalidAddress, "no available address")
	}

	return result, nil
}

// The crc32HashPicker is the default implementation of Picker.
// It will pick an Addr by using the crc32 hash algorithm.
type crc32HashPicker struct{}

func (p *crc32HashPicker) Pick(addrs []*Addr, _, key []byte) (*Addr, error) {
	n := len(addrs)
	if n == 0 {
		return nil, errors.Wrap(ErrInvalidAddress, "no available address")
	}
	if n == 1 {
		return addrs[0], nil
	}

	sum := crc32.ChecksumIEEE(key)
	return addrs[sum%uint32(n)], nil
}

type crc32HashPickBuilder struct{}

// NewCr32HashPickBuilder returns a crc32 hash picker builder.
func NewCr32HashPickBuilder() Builder {
	return crc32HashPickBuilder{}
}

func (b crc32HashPickBuilder) Build(_ []*Addr) Picker {
	return &crc32HashPicker{}
}

// The murmur3HashPicker is the implementation of Picker using murmur3 hash algorithm.
type murmur3HashPicker struct {
	hash func([]byte) uint64
}

func (p *murmur3HashPicker) Pick(addrs []*Addr, _, key []byte) (*Addr, error) {
	n := len(addrs)
	if n == 0 {
		return nil, errors.Wrap(ErrInvalidAddress, "no available address")
	}
	if n == 1 {
		return addrs[0], nil
	}

	sum := p.hash(key)
	return addrs[sum%uint64(n)], nil
}

type murmur3HashPickBuilder struct {
	seed uint64
}

// NewMurmur3HashPickBuilder creates a new Builder with the given seed.
func NewMurmur3HashPickBuilder(seed uint64) Builder {
	return murmur3HashPickBuilder{
		seed: seed,
	}
}

func (b murmur3HashPickBuilder) Build(_ []*Addr) Picker {
	return &murmur3HashPicker{
		hash: hash.NewMurmur3(b.seed).Hash,
	}
}

// The rendezvousHashPicker is the implementation of Picker using rendezvous hash algorithm.
// It is also known as HRW (Highest Random Weight) hash algorithm which
// is used to select a node in a distributed system.
//
// Its advantage is that if we have one more node offline, the data to be 'rebalance' would
// affect less than the other hash algorithms.
//
// For example, if we have 3 nodes, before one node offline, the data distribution is:
// key1(hash=0, node1), key2(hash=1, node2), key3(hash=2, node3)
//
// Once node1 offline, the data distribution is:
// key1(hash=0, node2), key2(hash=1, node3), key3(hash=2, node1)
// about 2/3 keys was affected.
//
// But if we use HRW hash algorithm, the data distribution would be:
// key1(node2 highest, node2) key2(node2 highest, node2), key3(node3 highest, node3)
// only 1/3 keys was affected.
type rendezvousHashPicker struct {
	hash func([]byte) uint64
}

func (p *rendezvousHashPicker) Pick(addrs []*Addr, _, key []byte) (*Addr, error) {
	highest := uint64(0)
	var winner int

	for idx, addr := range addrs {
		_addr := addr
		score := p.score(_addr, key)

		if score > highest {
			highest = score
			winner = idx
		} else if score == highest {
			// if the score is the same, we choose the one with the smaller address.
			highest = score
			// FIXED: use the higher priority address, to keep the highest score is stable.
			//  normally, the score can decide the winner, this case is rare.
			if _addr.Priority > addrs[winner].Priority {
				winner = idx
			}
		}
	}

	return addrs[winner], nil
}

func (p *rendezvousHashPicker) score(addr *Addr, key []byte) uint64 {
	_key := append(addr.shortcut(), key...)
	return p.hash(_key)
}

type rendezvousHashPickBuilder struct {
	hash func(key []byte) uint64
}

// NewRendezvousHashPickBuilder creates a new Builder with the given seed and hash function.
func NewRendezvousHashPickBuilder(seed uint64) Builder {
	return rendezvousHashPickBuilder{
		hash: hash.NewMurmur3(seed).Hash,
	}
}

// NewRendezvousHashPickBuilderWithHash creates a new Builder with the given hash function.
func NewRendezvousHashPickBuilderWithHash(hash func(key []byte) uint64) Builder {
	return rendezvousHashPickBuilder{
		hash: hash,
	}
}

func (b rendezvousHashPickBuilder) Build(_ []*Addr) Picker {
	return &rendezvousHashPicker{
		hash: b.hash,
	}
}
