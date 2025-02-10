package memcached

import (
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
)

func newDefaultResolver() Resolver {
	return defaultResolver{}
}

func Test_defaultResolver_Resolve(t *testing.T) {
	addrs, err := newDefaultResolver().Resolve("localhost:11211,localhost:11212,localhost:11213")
	if err != nil {
		t.Fatal(err)
	}

	if len(addrs) != 3 {
		t.Fatalf("expected 3 addrs, got %d", len(addrs))
	}

	for i, addr := range addrs {
		assert.Equal(t, "localhost:1121"+strconv.Itoa(i+1), addr.Address)
		assert.Equal(t, "tcp", addr.Network)
		assert.Equal(t, i, addr.Priority)
		assert.Equal(t, 0, len(addr.metadata))
	}
}

func Test_defaultResolver_Resolve_error(t *testing.T) {
	tests := []struct {
		name string
		addr string

		wantErr       bool
		wantAddrCount int
		wantAddresses map[string]struct{}
	}{
		{
			name:    "case1: empty address",
			addr:    "",
			wantErr: true,
		},
		{
			name:          "case2: invalid format address",
			addr:          "localhost:11211 , localhost:11212, localhost:11213,",
			wantErr:       false,
			wantAddrCount: 3,
			wantAddresses: map[string]struct{}{
				"localhost:11211": {},
				"localhost:11212": {},
				"localhost:11213": {},
			},
		},
		{
			name:    "case3: illegal format address",
			addr:    ":11211",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			addrs, err := newDefaultResolver().Resolve(tt.addr)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			assert.NoError(t, err)
			assert.Equal(t, tt.wantAddrCount, len(addrs))

			for _, addr := range addrs {
				assert.Contains(t, tt.wantAddresses, addr.Address)
			}
		})
	}
}

func Test_rendezvousHash_Pick(t *testing.T) {
	addrs, err := newDefaultResolver().Resolve("localhost:11211,localhost:11212,localhost:11213")
	assert.NoError(t, err)

	builder := NewRendezvousHashPickBuilder(120)
	picker := builder.Build(addrs)

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
			addr, err := picker.Pick(addrs, tt.args.cmd, tt.args.key)
			assert.NoError(t, err)
			assert.NotNil(t, addr)
			assert.Equal(t, tt.wantAddress, addr.Address)
		})
	}
}

func Test_rendezvousHash_Pick_stable(t *testing.T) {
	addrsBefore, err := newDefaultResolver().Resolve("localhost:11211,localhost:11212,localhost:11213")
	assert.NoError(t, err)

	builder := NewRendezvousHashPickBuilder(120)
	picker := builder.Build(addrsBefore)

	addr, err := picker.Pick(addrsBefore, "set", "key")
	assert.NoError(t, err)
	assert.NotNil(t, addr)
	assert.Equal(t, addrsBefore[0], addr)
	assert.Equal(t, "localhost:11211", addr.Address)

	// mock a node(localhost:11212) down
	addrsAfter, err := newDefaultResolver().Resolve("localhost:11211,localhost:11213")
	assert.NoError(t, err)

	addr2, err := picker.Pick(addrsAfter, "set", "key")
	assert.NoError(t, err)
	assert.NotNil(t, addr2)
	assert.Equal(t, "localhost:11211", addr.Address)
}
