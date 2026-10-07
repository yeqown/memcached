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

func TestResolveAddr(t *testing.T) {
	for _, test := range []struct {
		name, address            string
		wantNetwork, wantAddress string
		wantErr                  bool
	}{
		{name: "IPv4", address: "127.0.0.1:11211", wantNetwork: "tcp", wantAddress: "127.0.0.1:11211"},
		{name: "hostname", address: "google.com:11211", wantNetwork: "tcp", wantAddress: "google.com:11211"},
		{name: "IPv6", address: "[::1]:11211", wantNetwork: "tcp", wantAddress: "[::1]:11211"},
		{name: "Unix socket", address: "unix:///tmp/memcached.sock", wantNetwork: "unix", wantAddress: "/tmp/memcached.sock"},
		{name: "invalid address", address: "invalid_address", wantErr: true},
		{name: "empty address", wantErr: true},
		{name: "multiple addresses", address: "localhost:11211,localhost:11212,localhost:11213", wantErr: true},
		{name: "UDP", address: "udp://localhost:11211", wantNetwork: "udp", wantAddress: "localhost:11211"},
	} {
		t.Run(test.name, func(t *testing.T) {
			network, address, err := resolveAddr(test.address)
			if test.wantErr {
				require.ErrorIs(t, err, ErrInvalidAddress)
				return
			}
			require.NoError(t, err)
			require.Equal(t, test.wantNetwork, network)
			require.Equal(t, test.wantAddress, address)
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

func TestStaticRejectsDuplicateIdentity(t *testing.T) {
	_, _, err := NewStatic().Resolve(t.Context(), "CACHE.EXAMPLE.:11211,cache.example:011211")
	require.ErrorIs(t, err, ErrInvalidAddress)
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
