## Memcached

This is a golang package for Memcached. It is a simple and easy to use package.

### Features

- [ ] Completed Memcached text protocol, includes meta text protocol.
- [ ] Integrated serialization and deserialization function
- [x] Cluster support, multiple hash algorithm support, include: crc32, murmur3, redezvous and also custom hash algorithm.
- [x] Fully connection pool features support.

### Installation

```bash
go get github.com/yeqown/memcached@latest
```

### Usage

There is a simple example to show how to use this package. More examples could be found in the [example](./example) directory.

```go
func main() {
	// 1. build client
	// addrs is a string, if you have multiple memcached servers, you can use comma to separate them.
	// e.g. "localhost:11211,localhost:11212,localhost:11213"
	addrs := "localhost:11211"
	
	// client support options, you can set the options to client.
	// e.g. memcached.New(addrs, memcached.WithDialTimeout(5*time.Second))
	client, err := memcached.New(addrs)
	if err != nil {
		panic(err)
	}
	
	// 2. use client
	// now, you can use the client API to finish your work.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	version, err := client.Version(ctx)
	if err != nil {
		panic(err)
	}
	println("Version: ", version)
	
	if err = client.Set(ctx, "key", "value", 0, 0); err != nil {
		panic(err)
	}
	if err = client.Set(ctx, "key2", "value2", 0, 0); err != nil {
		panic(err)
	}

	items, err := client.Gets(ctx, "key", "key2")
	if err != nil {
		panic(err)
	}

	for _, item := range items {
		println("key: ", item.Key, " value: ", string(item.Value))
	}
}
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
| Touch | ✅      | `Touch(key string, expire int32) error` | Touch a key's expire time |
| Meta Get | 🚧      | `MetaGet(key string) (Meta, error)` | Get a key's meta information |
| Meta Set | 🚧      | `MetaSet(key string, meta Meta) error` | Set a key's meta information |
| Version | ✅      | `Version() (string, error)` | Get memcached server version |