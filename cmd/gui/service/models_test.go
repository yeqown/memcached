package service

import "testing"

func TestMemcachedServerAddressStripsLeadingSlash(t *testing.T) {
	s := MemcachedServer{Host: "/wl-local01-memcached01-1.offline-ops.net", Port: 11211}

	if got, want := s.Address(), "wl-local01-memcached01-1.offline-ops.net:11211"; got != want {
		t.Fatalf("Address() = %q, want %q", got, want)
	}
}
