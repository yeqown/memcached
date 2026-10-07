package picker

import (
	"encoding/binary"
	"strings"

	"github.com/pkg/errors"
	"github.com/yeqown/memcached/resolver"
)

// NewRendezvousHashPicker creates a rendezvous Picker using the seed. Scores
// depend only on node identity and key, not Priority or discovery result order.
// Upgrading from the former priority-based scoring changes existing key placement.
func NewRendezvousHashPicker(seed uint64) Picker {
	return NewRendezvousHashPickerWithHash(newMurmur3Hash(seed).sum64)
}

// NewRendezvousHashPickerWithHash creates a rendezvous Picker with the given hash
// function. The function must be deterministic, non-nil and support concurrent calls.
func NewRendezvousHashPickerWithHash(hash func(key []byte) uint64) Picker {
	return &rendezvousHashPicker{
		hash: hash,
	}
}

type rendezvousHashPicker struct{ hash func([]byte) uint64 }

func (p *rendezvousHashPicker) Pick(addrs []*resolver.Addr, _, key []byte) (*resolver.Addr, error) {
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
