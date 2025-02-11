## Memcached

This is a golang package for Memcached. It is a simple and easy to use package.

### Features

- [ ] Completed Memcached text protocol, includes meta text protocol.
- [ ] Integrated serialization and deserialization function
- [x] Cluster support, multiple hash algorithm support, include: crc32, murmur3, redezvous and also custom hash algorithm.
- [x] Connection pool support

### Installation

```bash
go get github.com/yeqown/memcached
```

### Usage

```go
TODO:
```

### Support Commands

Now, we have implemented some commands, and we will implement more commands in the future.

| Command | Status | Usage | Description |
| --- |--------| --- | --- |
| Set | ✅      | `Set(key string, value []byte, expire int32) error` | Set a key-value pair to memcached |
| Delete | ✅     | `Delete(key string) error` | Delete a key-value pair from memcached |
| Add | 🚧      | `Add(key string, value []byte, expire int32) error` | Add a key-value pair to memcached |
| Replace | 🚧      | `Replace(key string, value []byte, expire int32) error` | Replace a key-value pair to memcached |
| Append | 🚧      | `Append(key string, value []byte) error` | Append a value to the key |
| Prepend | 🚧      | `Prepend(key string, value []byte) error` | Prepend a value to the key |
| Cas | ✅      | `Cas(key string, value []byte, cas uint64, expire int32) error` | Compare and set a key-value pair to memcached |
| Gets | ✅      | `Gets(key string) ([]byte, error)` | Get a value by key from memcached with cas value |
| Get | ✅      | `Get(key string) ([]byte, error)` | Get a value by key from memcached |
| Increment | 🚧      | `Increment(key string, delta uint64) (uint64, error)` | Increment a key's value |
| Decrement | 🚧      | `Decrement(key string, delta uint64) (uint64, error)` | Decrement a key's value |
| Touch | 🚧      | `Touch(key string, expire int32) error` | Touch a key's expire time |
| Meta Get | 🚧      | `MetaGet(key string) (Meta, error)` | Get a key's meta information |
| Meta Set | 🚧      | `MetaSet(key string, meta Meta) error` | Set a key's meta information |
| Version | ✅      | `Version() (string, error)` | Get memcached server version |