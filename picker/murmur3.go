package picker

import (
	"github.com/pkg/errors"
	"github.com/yeqown/memcached/resolver"
)

// The murmur3HashPicker is the implementation of Picker using murmur3 hash algorithm.
type murmur3HashPicker struct {
	hash func([]byte) uint64
}

func (p *murmur3HashPicker) Pick(addrs []*resolver.Addr, _, key []byte) (*resolver.Addr, error) {
	n := len(addrs)
	if n == 0 {
		return nil, errors.Wrap(resolver.ErrInvalidAddress, "no available address")
	}
	if n == 1 {
		return addrs[0], nil
	}

	sum := p.hash(key)
	return addrs[sum%uint64(n)], nil
}

// NewMurmur3HashPicker creates a Picker using Murmur3 hashing with the given seed.
func NewMurmur3HashPicker(seed uint64) Picker {
	return &murmur3HashPicker{
		hash: newMurmur3Hash(seed).sum64,
	}
}

const (
	c1 = uint64(0x87c37b91114253d5)
	c2 = uint64(0x4cf5ad432745937f)
)

// murmur3Hash implements the existing 64-bit digest used by the pickers.
type murmur3Hash struct {
	seed uint64
}

// newMurmur3Hash creates a digest with the given seed.
func newMurmur3Hash(seed uint64) *murmur3Hash {
	return &murmur3Hash{seed: seed}
}

// sum64 returns the digest of the given key.
func (h *murmur3Hash) sum64(key []byte) uint64 {
	length := len(key)
	hash := h.seed

	// 处理主体部分
	nblocks := length / 8
	for i := 0; i < nblocks; i++ {
		k := uint64(key[i*8]) | uint64(key[i*8+1])<<8 |
			uint64(key[i*8+2])<<16 | uint64(key[i*8+3])<<24 |
			uint64(key[i*8+4])<<32 | uint64(key[i*8+5])<<40 |
			uint64(key[i*8+6])<<48 | uint64(key[i*8+7])<<56

		k *= c1
		k = (k << 31) | (k >> 33)
		k *= c2

		hash ^= k
		hash = (hash << 27) | (hash >> 37)
		hash = hash*5 + 0x52dce729
	}

	// 处理剩余字节
	tail := key[nblocks*8:]
	k2 := uint64(0)
	switch length & 7 {
	case 7:
		k2 ^= uint64(tail[6]) << 48
		fallthrough
	case 6:
		k2 ^= uint64(tail[5]) << 40
		fallthrough
	case 5:
		k2 ^= uint64(tail[4]) << 32
		fallthrough
	case 4:
		k2 ^= uint64(tail[3]) << 24
		fallthrough
	case 3:
		k2 ^= uint64(tail[2]) << 16
		fallthrough
	case 2:
		k2 ^= uint64(tail[1]) << 8
		fallthrough
	case 1:
		k2 ^= uint64(tail[0])
		k2 *= c1
		k2 = (k2 << 31) | (k2 >> 33)
		k2 *= c2
		hash ^= k2
	}

	// 最终混淆
	hash ^= uint64(length)
	hash ^= hash >> 33
	hash *= 0xff51afd7ed558ccd
	hash ^= hash >> 33
	hash *= 0xc4ceb9fe1a85ec53
	hash ^= hash >> 33

	return hash
}
