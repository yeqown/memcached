package picker

import (
	"encoding/binary"
	"strings"

	"github.com/pkg/errors"
	"github.com/yeqown/memcached/resolver"
)

// NewRendezvousHashPicker creates a rendezvous Picker using the seed. Scores
// depend only on node identity and key, independently of discovery result order.
// Upgrading from the former priority-based scoring changes existing key placement.
func NewRendezvousHashPicker(seed uint64) Picker {
	return &rendezvousHashPicker{builtin: newMurmur3Hash(seed)}
}

// NewRendezvousHashPickerWithHash creates a rendezvous Picker with the given hash
// function. The function must be deterministic, non-nil and support concurrent calls.
func NewRendezvousHashPickerWithHash(hash func(key []byte) uint64) Picker {
	return &rendezvousHashPicker{
		hash: hash,
	}
}

type rendezvousHashPicker struct {
	builtin *murmur3Hash
	hash    func([]byte) uint64
}

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
		score := p.score(addr, key)
		if winner == nil || score > highest || score == highest && identityLess(addr, winner) {
			winner, highest = addr, score
		}
	}
	return winner, nil
}

func (p *rendezvousHashPicker) score(addr *resolver.Addr, key []byte) uint64 {
	size := 2*binary.MaxVarintLen64 + len(addr.Network) + len(addr.Address) + len(key)
	if p.builtin == nil {
		// Custom hashes continue to receive an independent input for every node.
		return p.hash(appendRendezvousInput(make([]byte, 0, size), addr, key))
	}

	// A direct call to the built-in hash keeps common inputs on the stack.
	var buf [512]byte
	input := buf[:0]
	if size > len(buf) {
		input = make([]byte, 0, size)
	}
	return p.builtin.sum64(appendRendezvousInput(input, addr, key))
}

func appendRendezvousInput(input []byte, addr *resolver.Addr, key []byte) []byte {
	// Length framing keeps distinct network/address/key boundaries unambiguous.
	input = binary.AppendUvarint(input, uint64(len(addr.Network)))
	input = append(input, addr.Network...)
	input = binary.AppendUvarint(input, uint64(len(addr.Address)))
	input = append(input, addr.Address...)
	return append(input, key...)
}

func identityLess(a, b *resolver.Addr) bool {
	if n := strings.Compare(a.Network, b.Network); n != 0 {
		return n < 0
	}
	return a.Address < b.Address
}
