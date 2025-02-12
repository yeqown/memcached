## Memcached

[![Go Reference](https://pkg.go.dev/badge/github.com/yeqown/memcached.svg)](https://pkg.go.dev/github.com/yeqown/memcached) [![Go Report Card](https://goreportcard.com/badge/github.com/yeqown/memcached)](https://goreportcard.com/report/github.com/yeqown/memcached)  [![GitHub license](https://img.shields.io/github/license/yeqown/memcached)](https://github.com/yeqown/memcached/blob/master/LICENSE) [![GitHub release](https://img.shields.io/github/release/yeqown/memcached.svg)](https://github.com/yeqown/memcached/releases) [![GitHub stars](https://img.shields.io/github/stars/yeqown/memcached.svg)](https://github.com/yeqown/memcached/stargazers) [![GitHub issues](https://img.shields.io/github/issues/yeqown/memcached.svg)](https://github.com/yeqown/memcached/issues) [![Build Status](https://github.com/yeqown/memcached/workflows/Go/badge.svg)](https://github.com/yeqown/memcached/actions) [![codecov](https://codecov.io/gh/yeqown/memcached/branch/master/graph/badge.svg)](https://codecov.io/gh/yeqown/memcached)

This is a golang package for Memcached. It is a simple and easy to use package.

### Features

- [ ] Completed Memcached text protocol, includes meta text protocol.
- [ ] Integrated serialization and deserialization function
- [x] Cluster support, multiple hash algorithm support, include: crc32, murmur3, redezvous and also custom hash algorithm.
- [x] Fully connection pool features support.
- [ ] CLI tool support.

### Installation

```bash
go get github.com/yeqown/memcached@latest
```

Or you can install the CLI binary by running:

```bash
go install github.com/yeqown/memcached/cmd/memcached-cli@latest
```

### Usage

There is a simple example to show how to use this package. More examples could be found in the [example](./example) directory.

```go
package main

import (
    "context"
    "time"

    "github.com/yeqown/memcached"
)

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

| Command    | Status | API Usage                                                       | Description |
|------------|--------|-----------------------------------------------------------------| --- |
| Auth       | 🚧     | `Auth(username, password string) error`                         | Auth to memcached server |
| ----       | -----  | STORAGE COMMANDS                                                |---|
| Set        | ✅      | `Set(key string, value []byte, expire int32) error`             | Set a key-value pair to memcached |
| Add        | ✅      | `Add(key string, value []byte, expire int32) error`             | Add a key-value pair to memcached |
| Replace    | ✅      | `Replace(key string, value []byte, expire int32) error`         | Replace a key-value pair to memcached |
| Append     | ✅      | `Append(key string, value []byte) error`                        | Append a value to the key |
| Prepend    | ✅      | `Prepend(key string, value []byte) error`                       | Prepend a value to the key |
| Cas        | ✅      | `Cas(key string, value []byte, cas uint64, expire int32) error` | Compare and set a key-value pair to memcached |
| ----       | -----  | RETRIEVAL COMMANDS                                              |---|
| Gets       | ✅      | `Gets(key string) ([]byte, error)`                              | Get a value by key from memcached with cas value |
| Get        | ✅      | `Get(key string) ([]byte, error)`                               | Get a value by key from memcached |
| GetAndTouch | ✅      | `GetAndTouch(key string, expire int32) ([]byte, error)`         | Get a value by key from memcached and touch the key's expire time |
| GetAndTouches | ✅      | `GetAndTouchs(key string, expire int32) ([]byte, error)`        | Get a value by key from memcached and touch the key's expire time |
| -----      | -----  | OTHER COMMANDS                                                  |---|
| Delete     | ✅      | `Delete(key string) error`                                      | Delete a key-value pair from memcached |
| Incr       | ✅      | `Increment(key string, delta uint64) (uint64, error)`           | Increment a key's value |
| Decr       | ✅      | `Decrement(key string, delta uint64) (uint64, error)`           | Decrement a key's value |
| Touch      | ✅      | `Touch(key string, expire int32) error`                         | Touch a key's expire time |
| MetaGet    | ✅       | `MetaGet(key string) (Meta, error)`                             | Get a key's meta information |
| MetaSet    | ✅     | `MetaSet(key string, meta Meta) error`                          | Set a key's meta information |
| MetaDelete | 🚧     | `MetaDelete(key string) error`                                  | Delete a key's meta information |
| MetaArithmetic | 🚧     | `MetaArithmetic(key string, delta int64) (uint64, error)`       | Arithmetic a key's meta information |
| MetaDebug  | 🚧     | `MetaDebug(key string) (string, error)`                         | Debug a key's meta information |
| MetaNoop   | 🚧     | `MetaNoop(key string) error`                                    | Noop a key's meta information |
| Version    | ✅      | `Version() (string, error)`                                     | Get memcached server version |

### Development Guide

#### Prerequisites

- Go 1.21 or higher
- Python (for pre-commit hooks) or just `brew install pre-commit` on MacOS
- Docker (for running memcached in tests)

#### Setting up development environment

1. Clone the repository:
    ```bash
    git clone https://github.com/yeqown/memcached.git
    cd memcached
    ```
2. Install pre-commit hooks:
    ```bash
    pip install pre-commit
    # or MacOS
    # brew install pre-commit

    pre-commit install
    ```
3. Install golangci-lint:
    ```bash
    go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
    ```

#### Running Tests

```bash
go test -v -race -coverprofile=coverage.txt -covermode=atomic ./...
```

#### Code Style

This project follows the standard Go code style guidelines and uses golangci-lint for additional checks. The configuration can be found in [.golangci.yml](./.golangci.yml).

Key points:
- Follow Go standard formatting (enforced by gofmt )
- Ensure all code is properly tested
- Write clear commit messages
- Document public APIs
