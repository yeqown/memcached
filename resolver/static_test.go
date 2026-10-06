package resolver

import (
	"context"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStatic_Resolve(t *testing.T) {
	result, next, err := NewStatic().Resolve(context.Background(), "localhost:11211,localhost:11212,localhost:11213")
	if err != nil {
		t.Fatal(err)
	}

	addrs := result.Addrs
	require.Nil(t, next)
	if len(addrs) != 3 {
		t.Fatalf("expected 3 addrs, got %d", len(addrs))
	}

	for i, addr := range addrs {
		assert.Equal(t, "localhost:1121"+strconv.Itoa(i+1), addr.Address)
		assert.Equal(t, "tcp", addr.Network)
		assert.Equal(t, i, addr.Priority)
	}
}

func TestStatic_resolveAddr(t *testing.T) {
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
			network, addrs, err := resolveAddr(tt.args.addr)
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

func TestStatic_cancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, next, err := NewStatic().Resolve(ctx, "localhost:11211")
	require.ErrorIs(t, err, context.Canceled)
	require.Nil(t, next)
}

func TestStatic_addressValidation(t *testing.T) {
	for _, addr := range []string{
		"host:0", "host:65536", "host:abc", "host with space:11211", "[bad:ipv6]:11211", "unix://",
		"bad#host:11211", "bad..host:11211", "-bad.host:11211", "bad-.host:11211", "999.999.999.999:11211",
		"tcp6://[::ffff:127.0.0.1]:11211", "udp6://[::ffff:127.0.0.1]:11211",
	} {
		t.Run(addr, func(t *testing.T) {
			_, _, err := NewStatic().Resolve(context.Background(), addr)
			require.Error(t, err)
		})
	}
	result, next, err := NewStatic().Resolve(context.Background(), "CACHE.EXAMPLE.:011211,[0:0:0:0:0:0:0:1]:11211")
	require.NoError(t, err)
	require.Nil(t, next)
	require.Equal(t, "cache.example:11211", result.Addrs[0].Address)
	require.Equal(t, "[::1]:11211", result.Addrs[1].Address)
}

func TestStatic_canonicalizesMappedIPv4(t *testing.T) {
	for _, network := range []string{"tcp", "tcp4", "udp", "udp4"} {
		t.Run(network, func(t *testing.T) {
			result, _, err := NewStatic().Resolve(context.Background(), network+"://[::ffff:127.0.0.1]:11211")
			require.NoError(t, err)
			require.Equal(t, "127.0.0.1:11211", result.Addrs[0].Address)
		})
	}
}
