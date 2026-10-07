package picker

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/yeqown/memcached/resolver"
)

func TestBuiltInPickersEmptyAndSingleNode(t *testing.T) {
	for name, p := range map[string]Picker{
		"crc32":      NewCRC32HashPicker(),
		"murmur3":    NewMurmur3HashPicker(42),
		"rendezvous": NewRendezvousHashPicker(42),
	} {
		t.Run(name, func(t *testing.T) {
			addr, err := p.Pick(nil, []byte("get"), []byte("key"))
			require.ErrorIs(t, err, resolver.ErrInvalidAddress)
			require.Nil(t, addr)
			only := resolver.NewAddr("tcp", "cache.example:11211", 0)
			addr, err = p.Pick([]*resolver.Addr{only}, []byte("get"), []byte("key"))
			require.NoError(t, err)
			require.Same(t, only, addr)
		})
	}
}
