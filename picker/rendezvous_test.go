package picker

import (
	"context"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yeqown/memcached/resolver"
)

func Test_rendezvousHash_Pick(t *testing.T) {
	result, next, err := resolver.NewStatic().Resolve(context.Background(), "localhost:11211,localhost:11212,localhost:11213")
	assert.NoError(t, err)
	require.Nil(t, next)
	addrs := result.Addrs

	picker := NewRendezvousHashPicker(120)

	type args struct {
		cmd string
		key string
	}
	tests := []struct {
		name string
		args args

		wantIndex   int
		wantAddress string
	}{
		{
			name: "case1: normal",
			args: args{
				cmd: "set",
				key: "key",
			},
			wantIndex:   0,
			wantAddress: "localhost:11211",
		},
		{
			name: "case2: normal",
			args: args{
				cmd: "set",
				key: "key1",
			},
			wantIndex:   2,
			wantAddress: "localhost:11213",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			addr, err := picker.Pick(addrs, []byte(tt.args.cmd), []byte(tt.args.key))
			assert.NoError(t, err)
			assert.NotNil(t, addr)
			assert.Equal(t, tt.wantAddress, addr.Address)
		})
	}
}

func Test_rendezvousHash_Pick_stable(t *testing.T) {
	before, _, err := resolver.NewStatic().Resolve(context.Background(), "localhost:11211,localhost:11212,localhost:11213")
	assert.NoError(t, err)
	addrsBefore := before.Addrs

	picker := NewRendezvousHashPicker(120)

	addr, err := picker.Pick(addrsBefore, []byte("set"), []byte("key"))
	assert.NoError(t, err)
	assert.NotNil(t, addr)
	assert.Equal(t, addrsBefore[0], addr)
	assert.Equal(t, "localhost:11211", addr.Address)

	// mock a node(localhost:11212) down
	after, _, err := resolver.NewStatic().Resolve(context.Background(), "localhost:11211,localhost:11213")
	assert.NoError(t, err)
	addrsAfter := after.Addrs

	addr2, err := picker.Pick(addrsAfter, []byte("set"), []byte("key"))
	assert.NoError(t, err)
	assert.NotNil(t, addr2)
	assert.Equal(t, "localhost:11211", addr.Address)
}

func TestStableRendezvousMembershipAndOrdering(t *testing.T) {
	p := NewStableRendezvousHashPicker(42)
	before := []*resolver.Addr{resolver.NewAddr("tcp", "a:11211", 0), resolver.NewAddr("tcp", "b:11211", 1), resolver.NewAddr("tcp", "c:11211", 2)}
	expanded := []*resolver.Addr{resolver.NewAddr("tcp", "d:11211", 0), resolver.NewAddr("tcp", "c:11211", 1), resolver.NewAddr("tcp", "a:11211", 2), resolver.NewAddr("tcp", "b:11211", 3)}
	removed := []*resolver.Addr{resolver.NewAddr("tcp", "c:11211", 10), resolver.NewAddr("tcp", "a:11211", 11)}
	for i := range 2000 {
		key := []byte(strconv.Itoa(i))
		a, err := p.Pick(before, nil, key)
		require.NoError(t, err)
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
	_, err := p.Pick(nil, nil, nil)
	require.ErrorIs(t, err, resolver.ErrInvalidAddress)
}

func TestStableRendezvousTieByIdentity(t *testing.T) {
	p := &stableRendezvousHashPicker{hash: func([]byte) uint64 { return 0 }}
	for _, nodes := range [][]*resolver.Addr{
		{resolver.NewAddr("tcp", "b:11211", 100), resolver.NewAddr("tcp", "a:11211", 0)},
		{resolver.NewAddr("tcp", "a:11211", 0), resolver.NewAddr("tcp", "b:11211", 100)},
	} {
		a, err := p.Pick(nodes, nil, []byte("key"))
		require.NoError(t, err)
		require.Equal(t, "a:11211", a.Address)
	}
}
