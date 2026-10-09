package picker

import (
	"hash/crc32"

	"github.com/pkg/errors"
	"github.com/yeqown/memcached/resolver"
)

// The crc32HashPicker is the default implementation of Picker.
// It will pick an resolver.Addr by using the crc32 hash algorithm.
type crc32HashPicker struct{}

func (p *crc32HashPicker) Pick(addrs []*resolver.Addr, _, key []byte) (*resolver.Addr, error) {
	n := len(addrs)
	if n == 0 {
		return nil, errors.Wrap(resolver.ErrInvalidAddress, "no available address")
	}
	if n == 1 {
		return addrs[0], nil
	}

	sum := crc32.ChecksumIEEE(key)
	return addrs[sum%uint32(n)], nil
}

// NewCRC32HashPicker creates a Picker using CRC32 hashing.
func NewCRC32HashPicker() Picker {
	return &crc32HashPicker{}
}
