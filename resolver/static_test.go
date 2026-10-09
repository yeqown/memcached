package resolver

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStaticResolve(t *testing.T) {
	for _, test := range []struct {
		name, target string
		want         []*Addr
	}{
		{"IPv4", "127.0.0.1:11211", []*Addr{NewAddr("tcp", "127.0.0.1:11211", 0)}},
		{"hostname", "google.com:11211", []*Addr{NewAddr("tcp", "google.com:11211", 0)}},
		{"IPv6", "[::1]:11211", []*Addr{NewAddr("tcp", "[::1]:11211", 0)}},
		{"canonical hostname", "CACHE.EXAMPLE.:011211", []*Addr{NewAddr("tcp", "cache.example:11211", 0)}},
		{"canonical IPv6", "[0:0:0:0:0:0:0:1]:11211", []*Addr{NewAddr("tcp", "[::1]:11211", 0)}},
		{"Unix socket", "unix:///tmp/memcached.sock", []*Addr{NewAddr("unix", "/tmp/memcached.sock", 0)}},
		{"UDP", "udp://localhost:11211", []*Addr{NewAddr("udp", "localhost:11211", 0)}},
		{"multiple addresses", "localhost:11211,localhost:11212,localhost:11213", []*Addr{
			NewAddr("tcp", "localhost:11211", 0), NewAddr("tcp", "localhost:11212", 1), NewAddr("tcp", "localhost:11213", 2),
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			result, next, err := NewStatic().Resolve(t.Context(), test.target)
			require.NoError(t, err)
			require.Nil(t, next, "static resolution never schedules a refresh")
			require.Equal(t, ResolveResult{Addrs: test.want}, result)
		})
	}
}

func TestStaticInvalidTarget(t *testing.T) {
	for name, target := range map[string]string{
		"invalid address":            "invalid_address",
		"empty address":              "",
		"blank address list":         " , \t, ",
		"zero port":                  "host:0",
		"port overflow":              "host:65536",
		"non-numeric port":           "host:abc",
		"invalid IPv6":               "[bad:ipv6]:11211",
		"empty Unix path":            "unix://",
		"invalid hostname character": "bad#host:11211",
		"duplicate identity":         "CACHE.EXAMPLE.:11211,cache.example:011211",
		"invalid member":             "localhost:11211,invalid_address",
	} {
		t.Run(name, func(t *testing.T) {
			result, next, err := NewStatic().Resolve(t.Context(), target)
			require.ErrorIs(t, err, ErrInvalidAddress)
			require.Empty(t, result)
			require.Nil(t, next)
		})
	}
}

func TestStaticCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	result, next, err := NewStatic().Resolve(ctx, "localhost:11211")
	require.ErrorIs(t, err, context.Canceled)
	require.Empty(t, result)
	require.Nil(t, next)
}
