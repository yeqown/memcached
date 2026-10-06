package picker

import (
	"encoding/binary"
	"strconv"
	"strings"

	"github.com/pkg/errors"
	"github.com/yeqown/memcached/resolver"
)

// rendezvousHashPicker preserves the legacy scores based on transport, address,
// priority and key. Highest scores win; equal scores prefer higher priority.
type rendezvousHashPicker struct {
	hash func([]byte) uint64
}

func (p *rendezvousHashPicker) Pick(addrs []*resolver.Addr, _, key []byte) (*resolver.Addr, error) {
	if len(addrs) == 0 {
		return nil, errors.Wrap(resolver.ErrInvalidAddress, "no available address")
	}
	highest := uint64(0)
	var winner int

	for idx, addr := range addrs {
		_addr := addr
		score := p.score(_addr, key)

		if score > highest {
			highest = score
			winner = idx
		} else if score == highest {
			highest = score
			if _addr.Priority > addrs[winner].Priority {
				winner = idx
			}
		}
	}

	return addrs[winner], nil
}

func (p *rendezvousHashPicker) score(addr *resolver.Addr, key []byte) uint64 {
	_key := append([]byte(addr.Network+"-"+addr.Address+strconv.Itoa(addr.Priority)), key...)
	return p.hash(_key)
}

// NewRendezvousHashPicker creates a legacy rendezvous Picker using the seed.
func NewRendezvousHashPicker(seed uint64) Picker {
	return NewRendezvousHashPickerWithHash(newMurmur3Hash(seed).sum64)
}

// NewRendezvousHashPickerWithHash creates a legacy rendezvous Picker with the
// given hash function. The function must be non-nil and support concurrent calls.
func NewRendezvousHashPickerWithHash(hash func(key []byte) uint64) Picker {
	return &rendezvousHashPicker{
		hash: hash,
	}
}

// NewStableRendezvousHashPicker constructs a rendezvous Picker whose scores
// depend only on node identity and key, not Priority or discovery result order.
// Switching from the legacy rendezvous Picker changes existing key placement.
func NewStableRendezvousHashPicker(seed uint64) Picker {
	return &stableRendezvousHashPicker{hash: newMurmur3Hash(seed).sum64}
}

type stableRendezvousHashPicker struct{ hash func([]byte) uint64 }

func (p *stableRendezvousHashPicker) Pick(addrs []*resolver.Addr, _, key []byte) (*resolver.Addr, error) {
	if len(addrs) == 0 {
		return nil, errors.Wrap(resolver.ErrInvalidAddress, "empty topology")
	}
	var winner *resolver.Addr
	var highest uint64
	for _, addr := range addrs {
		if addr == nil {
			return nil, errors.Wrap(resolver.ErrInvalidAddress, "nil node")
		}
		// Length framing keeps distinct network/address/key boundaries unambiguous.
		input := binary.AppendUvarint(nil, uint64(len(addr.Network)))
		input = append(input, addr.Network...)
		input = binary.AppendUvarint(input, uint64(len(addr.Address)))
		input = append(input, addr.Address...)
		input = append(input, key...)
		score := p.hash(input)
		if winner == nil || score > highest || score == highest && identityLess(addr, winner) {
			winner, highest = addr, score
		}
	}
	return winner, nil
}

func identityLess(a, b *resolver.Addr) bool {
	if n := strings.Compare(a.Network, b.Network); n != 0 {
		return n < 0
	}
	return a.Address < b.Address
}
