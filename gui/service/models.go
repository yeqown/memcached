package service

import (
	"fmt"
	"strings"
)

// MemcachedServer represents a single memcached server instance.
type MemcachedServer struct {
	Host string `json:"host"`
	Port int    `json:"port"`
}

// Address returns the host:port string for connecting.
func (s MemcachedServer) Address() string {
	return fmt.Sprintf("%s:%d", strings.TrimPrefix(strings.TrimSpace(s.Host), "/"), s.Port)
}

// Context represents a group of memcached servers (a cluster).
type Context struct {
	ID      string            `json:"id"`
	Name    string            `json:"name"`
	Servers []MemcachedServer `json:"servers"`
}

// OperationResult is the unified response for memcached operations.
type OperationResult struct {
	Success          bool   `json:"success"`
	Data             string `json:"data"`
	Error            string `json:"error,omitempty"`
	Key              string `json:"key,omitempty"`
	Value            string `json:"value,omitempty"`
	TTL              int64  `json:"ttl,omitempty"`
	LastAccessedTime int64  `json:"lastAccessedTime,omitempty"`
	CAS              uint64 `json:"cas,omitempty"`
	Flags            uint16 `json:"flags,omitempty"`
	Size             uint64 `json:"size,omitempty"`
	HitBefore        bool   `json:"hitBefore,omitempty"`
	Opaque           uint64 `json:"opaque,omitempty"`
	ValueKind        string `json:"valueKind,omitempty"`
}
