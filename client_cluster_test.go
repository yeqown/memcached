package memcached

import (
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
)

func newDefaultResolver() defaultResolver {
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

func Test_defaultResolver_resolveAddr(t *testing.T) {
	type args struct {
		addr string
	}

	tests := []struct {
		name        string
		args        args
		wantErr     bool
		wantNetwork string
		wantAddress string
	}{
		{
			name: "case1: v4",
			args: args{
				addr: "localhost:11211",
			},
			wantErr:     false,
			wantNetwork: "tcp",
			wantAddress: "localhost:11211",
		},
		{
			name: "case1: v4 with domain host",
			args: args{
				addr: "google.com:11211",
			},
			wantErr:     false,
			wantNetwork: "tcp",
			wantAddress: "google.com:11211",
		},
		{
			name: "case2: ip v6",
			args: args{
				addr: "[::1]:11211",
			},
			wantErr:     false,
			wantNetwork: "tcp",
			wantAddress: "[::1]:11211",
		},
		{
			name: "case3: unix socket, not supported yet",
			args: args{
				addr: "unix:///tmp/memcached.sock",
			},
			wantErr:     false,
			wantNetwork: "unix",
			wantAddress: "/tmp/memcached.sock",
		},
		{
			name: "case4: invalid address",
			args: args{
				addr: "invalid_address",
			},
			wantErr: true,
		},
		{
			name: "case5: empty address",
			args: args{
				addr: "",
			},
			wantErr: true,
		},
		{
			name: "case6: multiple addresses",
			args: args{
				addr: "localhost:11211,localhost:11212,localhost:11213",
			},
			wantErr: true,
		},
		{
			name: "case7: udp",
			args: args{
				addr: "udp://localhost:11211",
			},
			wantErr:     false,
			wantNetwork: "udp",
			wantAddress: "localhost:11211",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			network, addrs, err := newDefaultResolver().resolveAddr(tt.args.addr)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			assert.NoError(t, err)
			assert.Equal(t, tt.wantNetwork, network)
			assert.Equal(t, tt.wantAddress, addrs)
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
			addr, err := picker.Pick(addrs, []byte(tt.args.cmd), []byte(tt.args.key))
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

	addr, err := picker.Pick(addrsBefore, []byte("set"), []byte("key"))
	assert.NoError(t, err)
	assert.NotNil(t, addr)
	assert.Equal(t, addrsBefore[0], addr)
	assert.Equal(t, "localhost:11211", addr.Address)

	// mock a node(localhost:11212) down
	addrsAfter, err := newDefaultResolver().Resolve("localhost:11211,localhost:11213")
	assert.NoError(t, err)

	addr2, err := picker.Pick(addrsAfter, []byte("set"), []byte("key"))
	assert.NoError(t, err)
	assert.NotNil(t, addr2)
	assert.Equal(t, "localhost:11211", addr.Address)
}
