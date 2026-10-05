# memcached

[English](./README.md) | [简体中文](./README.zh-CN.md)

[![Go Reference](https://pkg.go.dev/badge/github.com/yeqown/memcached.svg)](https://pkg.go.dev/github.com/yeqown/memcached) [![Build Status](https://github.com/yeqown/memcached/workflows/Go/badge.svg)](https://github.com/yeqown/memcached/actions) [![License](https://img.shields.io/github/license/yeqown/memcached)](./LICENSE)

A Go client for Memcached's text and meta text protocols. It provides context-aware operations, configurable multi-node routing and connection pools, value codecs, and optional OpenTelemetry instrumentation. Requires Go 1.26 or newer.

## Compared with gomemcache

Both clients support basic text commands, CAS, multi-server routing, connection reuse, TCP, and Unix sockets. Here is what this package adds over [bradfitz/gomemcache](https://github.com/bradfitz/gomemcache)'s core API:

| Capability | bradfitz/gomemcache | This package |
| --- | --- | --- |
| Per-operation context and deadlines | `Get(key)` / `Set(item)` have no call context | `Get(ctx, key)` / `Set(ctx, ...)`; context deadlines and separate dial, read, and write timeouts |
| Meta text protocol | No meta command API | `MetaGet`, `MetaSet`, `MetaDelete`, `MetaArithmetic`, `MetaDebug`, `MetaNoOp`; CAS, TTL, stale and recache flags |
| Connection limits | Configurable `MaxIdleConns` | Maximum open and idle connections per node, lifetime, and idle timeout |
| Value compression | No built-in codec | Pluggable `Codec`; MC-COMPRESS-compatible Deflate, LZ4, Snappy, and Zstd |
| OpenTelemetry | No built-in instrumentation | Opt-in tracing and operation metrics |
| Built-in key routing | CRC32 `ServerList` or a custom selector | CRC32, Murmur3, rendezvous hashing, or a custom resolver/picker |
| Tools | Client library | Interactive [CLI](./cmd/memcached-cli/README.md) and [Wails GUI](./gui/README.md) |

Comparison checked against gomemcache [revision `24af94b`](https://github.com/bradfitz/gomemcache/tree/24af94b03874); later upstream changes may differ.

The API also provides `GetAndTouch`, `GetAndTouches`, `Stats`, `Version`, and the usual storage, retrieval, deletion, and counter commands. UDP is available as an opt-in transport. See the [usage guide](./docs/usage.md) for the full command list and operational limits.

## Quick start

Start a Memcached server at `localhost:11211`, then install the client:

```bash
go get github.com/yeqown/memcached@latest
```

```go
package main

import (
	"context"
	"fmt"
	"time"

	"github.com/yeqown/memcached"
)

func main() {
	client, err := memcached.New("localhost:11211",
		memcached.WithDialTimeout(2*time.Second),
		memcached.WithReadTimeout(3*time.Second),
		memcached.WithWriteTimeout(3*time.Second),
		memcached.WithMaxConns(64),
		memcached.WithMaxIdleConns(16),
		memcached.WithMaxLifetime(time.Hour),
		memcached.WithMaxIdleTimeout(5*time.Minute),
	)
	if err != nil {
		panic(err)
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Set(ctx, "greeting", []byte("hello"), 0, time.Minute); err != nil {
		panic(err)
	}
	item, err := client.Get(ctx, "greeting")
	if err != nil {
		panic(err)
	}
	fmt.Println(string(item.Value)) // hello
}
```

The values above are examples to tune for your workload. `New` also accepts comma-separated server addresses and composable options:

| To configure | Address or option |
| --- | --- |
| Multiple nodes and key placement | `"cache-1:11211,cache-2:11211"` plus `memcached.WithPickBuilder(memcached.NewRendezvousHashPickBuilder(0))` |
| Custom address resolution | `memcached.WithResolver(resolver)` |
| Compression or another value codec | `memcached.WithCodec(codec)` |
| Traces and metrics | `memcached.WithTelemetry(telemetry.WithTracerProvider(tp), telemetry.WithMeterProvider(mp))` |
| Writes without server acknowledgments | `memcached.WithNoReply()` |
| Legacy UDP transport | A `udp://` address plus `memcached.WithUDPEnabled()` |

See the [usage guide](./docs/usage.md) for codec and telemetry setup. With multiple servers, `Gets` and `GetAndTouches` send all requested keys to one node; use per-key reads unless the keys are known to reside together.

## More

- [Usage guide: commands, routing, codecs, telemetry, and transport](./docs/usage.md)
- [Examples](./example/) and [Go API reference](https://pkg.go.dev/github.com/yeqown/memcached)
- [CLI installation and commands](./cmd/memcached-cli/README.md)
- [MC-COMPRESS flag format](./docs/MC-COMPRESS-SPEC-v1.0.md)
