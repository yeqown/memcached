package picker

import (
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/yeqown/memcached/resolver"
)

func TestRendezvousPreservesKeyPlacement(t *testing.T) {
	addrs := []*resolver.Addr{
		resolver.NewAddr("tcp", "a:11211"),
		resolver.NewAddr("tcp", "b:11211"),
		resolver.NewAddr("tcp6", "[::1]:11211"),
	}
	for _, test := range []struct {
		seed uint64
		key  string
		want int
	}{
		{seed: 0, key: "", want: 2},
		{seed: 0, key: "key", want: 2},
		{seed: 0, key: "user:42", want: 2},
		{seed: 0, key: "中文", want: 1},
		{seed: 0, key: strings.Repeat("k", 300), want: 2},
		{seed: 0, key: strings.Repeat("k", 600), want: 1},
		{seed: 42, key: "", want: 0},
		{seed: 42, key: "key", want: 1},
		{seed: 42, key: "user:42", want: 1},
		{seed: 42, key: "中文", want: 2},
		{seed: 42, key: strings.Repeat("k", 300), want: 0},
		{seed: 42, key: strings.Repeat("k", 600), want: 0},
	} {
		t.Run(fmt.Sprintf("seed=%d/key=%s", test.seed, test.key), func(t *testing.T) {
			addr, err := NewRendezvousHashPicker(test.seed).Pick(addrs, nil, []byte(test.key))
			require.NoError(t, err)
			require.Same(t, addrs[test.want], addr)
		})
	}
}

func TestRendezvousCommonKeysDoNotAllocate(t *testing.T) {
	p := NewRendezvousHashPicker(42)
	addrs := []*resolver.Addr{resolver.NewAddr("tcp", "a:11211"), resolver.NewAddr("tcp6", "[::1]:11211")}
	key := []byte("user:42")
	allocs := testing.AllocsPerRun(100, func() {
		if _, err := p.Pick(addrs, nil, key); err != nil {
			t.Fatal(err)
		}
	})
	require.Zero(t, allocs, "routing common keys with the built-in hash must not allocate")
}

func TestRendezvousCustomHashInput(t *testing.T) {
	var inputs [][]byte
	p := NewRendezvousHashPickerWithHash(func(input []byte) uint64 {
		inputs = append(inputs, input)
		return 0
	})
	addrs := []*resolver.Addr{resolver.NewAddr("tcp", "a:11211"), resolver.NewAddr("tcp6", "[::1]:11211")}
	_, err := p.Pick(addrs, nil, []byte("key"))
	require.NoError(t, err)
	require.Equal(t, [][]byte{[]byte("\x03tcp\x07a:11211key"), []byte("\x04tcp6\x0b[::1]:11211key")}, inputs)
}

func BenchmarkRendezvousHashPicker(b *testing.B) {
	for _, size := range []int{3, 10, 50} {
		b.Run(fmt.Sprintf("nodes=%d", size), func(b *testing.B) {
			addrs := make([]*resolver.Addr, size)
			for i := range addrs {
				addrs[i] = resolver.NewAddr("tcp", fmt.Sprintf("10.0.0.%d:11211", i+1))
			}
			p := NewRendezvousHashPicker(1)
			key := []byte("review-key-0123456789")
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				if _, err := p.Pick(addrs, nil, key); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func TestRendezvousMembershipAndOrdering(t *testing.T) {
	p := NewRendezvousHashPicker(42)
	before := []*resolver.Addr{resolver.NewAddr("tcp", "a:11211"), resolver.NewAddr("tcp", "b:11211"), resolver.NewAddr("tcp", "c:11211")}
	expanded := []*resolver.Addr{resolver.NewAddr("tcp", "d:11211"), resolver.NewAddr("tcp", "c:11211"), resolver.NewAddr("tcp", "a:11211"), resolver.NewAddr("tcp", "b:11211")}
	removed := []*resolver.Addr{resolver.NewAddr("tcp", "c:11211"), resolver.NewAddr("tcp", "a:11211")}
	selected := make(map[resolver.Addr]struct{})
	for i := range 2000 {
		key := []byte(strconv.Itoa(i))
		a, err := p.Pick(before, nil, key)
		require.NoError(t, err)
		selected[*a] = struct{}{}
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
}

func TestRendezvousTieByIdentity(t *testing.T) {
	p := NewRendezvousHashPickerWithHash(func([]byte) uint64 { return 0 })
	a, b := resolver.NewAddr("tcp", "a:11211"), resolver.NewAddr("tcp", "b:11211")
	c := resolver.NewAddr("tcp4", "a:11211")
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
	valid := resolver.NewAddr("tcp", "a:11211")
	for _, test := range []struct {
		name  string
		nodes []*resolver.Addr
	}{
		{name: "only node", nodes: []*resolver.Addr{nil}},
		{name: "after valid node", nodes: []*resolver.Addr{valid, nil}},
	} {
		t.Run(test.name, func(t *testing.T) {
			addr, err := p.Pick(test.nodes, nil, []byte("key"))
			require.ErrorIs(t, err, resolver.ErrInvalidAddress)
			require.Nil(t, addr)
		})
	}
}
