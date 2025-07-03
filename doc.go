// Package memcached provides a memcached client implementation.
//
// The client is designed to be straightforward and easy to use, and it is thread-safe.
// This package supports the memcached text protocol including the following commands:
// - set/add/replace/append/prepend/cas
// - get/gets/gat/gats
// - ms/mg/md/md/mn/ma/me
// - delete
// - incr/decr
// - touch
// - version
// - auth
//
// The client also supports the cluster mode, which means that the client can connect
// to multiple memcached instances, and embed some hash algorithm to pick a memcached
// instance to execute a command.
package memcached
