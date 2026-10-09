package resolver

import (
	"errors"
	"net"
	"net/netip"
	"path/filepath"
	"strconv"
	"strings"

	pkgerrors "github.com/pkg/errors"
)

var (
	// ErrInvalidAddress is returned for invalid or unavailable node addresses.
	ErrInvalidAddress = errors.New("invalid address")
	// ErrInvalidNetworkProtocol is returned for unsupported transport networks.
	ErrInvalidNetworkProtocol = errors.New("invalid network protocol")
)

// Addr identifies a Memcached node by its network and address and can be used
// as a map key. Published addresses must be treated as immutable.
type Addr struct {
	Network string
	Address string
}

// NewAddr creates a node address with the given network and address.
func NewAddr(network, address string) *Addr {
	return &Addr{Network: network, Address: address}
}

// Clone copies the address.
func (a *Addr) Clone() *Addr {
	copyAddr := *a
	return &copyAddr
}

// canonicalAddress validates syntax without a DNS lookup. The dialer, or a
// dedicated discovery Resolver, performs network resolution with a Context.
func canonicalAddress(network, address string) (string, error) {
	switch network {
	case "unix":
		if address == "" || !filepath.IsAbs(address) || strings.ContainsRune(address, '\x00') {
			return "", pkgerrors.Wrap(ErrInvalidAddress, "invalid unix path")
		}
		return filepath.Clean(address), nil
	case "tcp", "tcp4", "tcp6", "udp", "udp4", "udp6":
	default:
		return "", ErrInvalidNetworkProtocol
	}
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return "", pkgerrors.Wrap(ErrInvalidAddress, err.Error())
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 || host == "" || strings.ContainsAny(host, " \t\r\n/,\x00") {
		return "", pkgerrors.Wrap(ErrInvalidAddress, "invalid host or port")
	}
	if ip, parseErr := netip.ParseAddr(host); parseErr == nil {
		// Go's dialer treats mapped IPv4 as IPv4, even in bracketed form.
		ip = ip.Unmap()
		if strings.HasSuffix(network, "4") && !ip.Is4() || strings.HasSuffix(network, "6") && !ip.Is6() {
			return "", pkgerrors.Wrap(ErrInvalidAddress, "IP family does not match network")
		}
		host = ip.String()
	} else {
		if strings.ContainsAny(host, ":%[]") {
			return "", pkgerrors.Wrap(ErrInvalidAddress, "invalid IP address")
		}
		host = strings.ToLower(strings.TrimSuffix(host, "."))
		if !validHostname(host) {
			return "", pkgerrors.Wrap(ErrInvalidAddress, "invalid hostname")
		}
	}
	return net.JoinHostPort(host, strconv.Itoa(n)), nil
}

// validHostname accepts Go-compatible DNS labels without performing a lookup.
// Purely numeric dotted input must be a valid IP literal, handled above.
func validHostname(host string) bool {
	if host == "" || len(host) > 253 {
		return false
	}
	nonNumeric := false
	for label := range strings.SplitSeq(host, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, ch := range label {
			switch {
			case ch >= 'a' && ch <= 'z', ch == '-', ch == '_':
				nonNumeric = true
			case ch >= '0' && ch <= '9':
			default:
				return false
			}
		}
	}
	return nonNumeric
}

// resolveAddr resolves single network address, supports tcp, udp and unix socket format.
func resolveAddr(address string) (network, addr string, err error) {
	address = strings.TrimSpace(address)
	if address == "" {
		return "", "", pkgerrors.Wrap(ErrInvalidAddress, "empty address")
	}

	network = "tcp"
	addr = address

	if prefix, suffix, ok := strings.Cut(address, "://"); ok {
		network, addr = prefix, suffix
	}
	addr, err = canonicalAddress(network, addr)
	if err != nil {
		return "", "", pkgerrors.Wrap(err, "invalid address: "+address)
	}

	return network, addr, nil
}
