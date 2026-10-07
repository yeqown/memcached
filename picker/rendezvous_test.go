package picker

import (
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/yeqown/memcached/resolver"
)

func TestRendezvousMembershipAndOrdering(t *testing.T) {
	p := NewRendezvousHashPicker(42)
	before := []*resolver.Addr{resolver.NewAddr("tcp", "a:11211", 0), resolver.NewAddr("tcp", "b:11211", 1), resolver.NewAddr("tcp", "c:11211", 2)}
	expanded := []*resolver.Addr{resolver.NewAddr("tcp", "d:11211", 0), resolver.NewAddr("tcp", "c:11211", 1), resolver.NewAddr("tcp", "a:11211", 2), resolver.NewAddr("tcp", "b:11211", 3)}
	removed := []*resolver.Addr{resolver.NewAddr("tcp", "c:11211", 10), resolver.NewAddr("tcp", "a:11211", 11)}
	selected := make(map[resolver.AddrKey]struct{})
	for i := range 2000 {
		key := []byte(strconv.Itoa(i))
		a, err := p.Pick(before, nil, key)
		require.NoError(t, err)
		selected[a.AddrKey] = struct{}{}
		b, err := p.Pick(expanded, nil, key)
		require.NoError(t, err)
		require.True(t, b.Address == "d:11211" || b.Address == a.Address, "expansion moved unrelated key %d", i)
		c, err := p.Pick(removed, nil, key)
		require.NoError(t, err)
		if a.Address != "b:11211" {
			require.Equal(t, a.Address, c.Address, "removal moved unrelated key %d", i)
		}
		shuffled, err := p.Pick([]*resolver.Addr{before[2], before[0], before[1]}, nil, key)
		require.NoError(t, err)
		require.Equal(t, a.Address, shuffled.Address)
	}
	require.Len(t, selected, len(before), "sampled keys must reach every node")
	_, err := p.Pick(nil, nil, nil)
	require.ErrorIs(t, err, resolver.ErrInvalidAddress)
}

func TestRendezvousTieByIdentity(t *testing.T) {
	p := NewRendezvousHashPickerWithHash(func([]byte) uint64 { return 0 })
	a, b := resolver.NewAddr("tcp", "a:11211", 0), resolver.NewAddr("tcp", "b:11211", 100)
	c := resolver.NewAddr("tcp4", "a:11211", 200)
	for _, test := range []struct {
		name  string
		nodes []*resolver.Addr
		want  *resolver.Addr
	}{
		{name: "address", nodes: []*resolver.Addr{b, a}, want: a},
		{name: "reversed addresses", nodes: []*resolver.Addr{a, b}, want: a},
		{name: "network before address", nodes: []*resolver.Addr{c, b}, want: b},
		{name: "reversed networks", nodes: []*resolver.Addr{b, c}, want: b},
	} {
		t.Run(test.name, func(t *testing.T) {
			addr, err := p.Pick(test.nodes, nil, []byte("key"))
			require.NoError(t, err)
			require.Same(t, test.want, addr)
		})
	}
}

func TestRendezvousRejectsNilNode(t *testing.T) {
	p := NewRendezvousHashPicker(42)
	valid := resolver.NewAddr("tcp", "a:11211", 0)
	for _, test := range []struct {
		name  string
		nodes []*resolver.Addr
	}{
		{name: "only node", nodes: []*resolver.Addr{nil}},
		{name: "after valid node", nodes: []*resolver.Addr{valid, nil}},
	} {
		t.Run(test.name, func(t *testing.T) {
			var addr *resolver.Addr
			var err error
			require.NotPanics(t, func() { addr, err = p.Pick(test.nodes, nil, []byte("key")) })
			require.ErrorIs(t, err, resolver.ErrInvalidAddress)
			require.Nil(t, addr)
		})
	}
}
