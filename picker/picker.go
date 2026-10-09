// Package picker provides node routing algorithms that operate on a client's
// current immutable address list.
package picker

import "github.com/yeqown/memcached/resolver"

// Picker selects a node from the supplied active address list. Pick must support
// concurrent calls and must not retain or modify the supplied addresses. A
// Picker may be shared across clients.
type Picker interface {
	Pick(addrs []*resolver.Addr, cmd, key []byte) (*resolver.Addr, error)
}
