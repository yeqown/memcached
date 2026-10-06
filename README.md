# memcached

[English](./README.md) | [简体中文](./README.zh-CN.md)

[![Go Reference](https://pkg.go.dev/badge/github.com/yeqown/memcached.svg)](https://pkg.go.dev/github.com/yeqown/memcached) [![Build Status](https://github.com/yeqown/memcached/workflows/Go/badge.svg)](https://github.com/yeqown/memcached/actions) [![License](https://img.shields.io/github/license/yeqown/memcached)](./LICENSE)

A Go client for Memcached's text and meta text protocols. It provides context-aware operations, resolver-driven node discovery, configurable multi-node routing and connection pools, value codecs, and optional OpenTelemetry instrumentation. Requires Go 1.26 or newer.

## Compared with gomemcache

Both clients support basic text commands, CAS, multi-server routing, connection reuse, TCP, and Unix sockets. Here is what this package adds over [bradfitz/gomemcache](https://github.com/bradfitz/gomemcache)'s core API:

| Capability | bradfitz/gomemcache | This package |
| --- | --- | --- |
| Per-operation context and deadlines | `Get(key)` / `Set(item)` have no call context | `Get(ctx, key)` / `Set(ctx, ...)`; context deadlines and separate dial, read, and write timeouts |
| Meta text protocol | No meta command API | `MetaGet`, `MetaSet`, `MetaDelete`, `MetaArithmetic`, `MetaDebug`, `MetaNoOp`; CAS and TTL options, but not all recache response markers are exposed |
| Connection limits | Reuses connections per address; configurable `MaxIdleConns` | Pools per node with `MaxConns`, lifetime, and idle timeout; concurrent dials reserve capacity before connecting |
| Value compression | No built-in codec | Pluggable `Codec`; MC-COMPRESS-compatible Deflate, LZ4, Snappy, and Zstd |
| OpenTelemetry | No built-in instrumentation | Opt-in tracing, operation metrics, and discovery/topology metrics |
| Built-in key routing | CRC32 `ServerList` or a custom selector | CRC32, Murmur3, rendezvous hashing, stable rendezvous hashing, or a custom picker |
| Node discovery | Configure addresses through a selector | The `resolver` package provides static addresses and AWS / Google discovery; custom resolvers decide the next refresh time |
| Tools | Client library | Interactive [CLI](./cmd/memcached-cli/README.md) and [Wails GUI](./gui/README.md) |

Comparison checked against gomemcache [revision `4d751bb`](https://github.com/bradfitz/gomemcache/tree/4d751bb6e37cf0da5fd57a86b880f76791307adf); later upstream changes may differ.

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
| Multiple nodes and key placement | `"cache-1:11211,cache-2:11211"` plus `memcached.WithPicker(picker.NewStableRendezvousHashPicker(0))` |
| Custom address resolution and discovery | `memcached.WithResolver(resolver)` and `memcached.WithResolveTimeout(5*time.Second)` |
| Compression or another value codec | `memcached.WithCodec(codec)` |
| Traces and metrics | `memcached.WithTelemetry(telemetry.WithTracerProvider(tp), telemetry.WithMeterProvider(mp))` |
| Writes without server acknowledgments | `memcached.WithNoReply()` |
| Legacy UDP transport | A `udp://` address plus `memcached.WithUDPEnabled()` |

See the [usage guide](./docs/usage.md) for codec and telemetry setup. With multiple servers, `Gets` and `GetAndTouches` send all requested keys to one node; use per-key reads unless the keys are known to reside together.

AWS ElastiCache Memcached and Google Memorystore Memcached use the same built-in resolver: pass the configuration / discovery endpoint, including its port, to `New` with `memcached.WithResolver(resolver.NewAutoDiscovery(time.Minute))`. It uses only `config get cluster`, applies the returned node topology, and preserves the last successful result on error. Non-positive intervals default to one minute. See the [automatic discovery guide](./docs/usage.md#aws-and-google-automatic-discovery).

## Resolver / Picker API migration

Resolver and picker APIs and built-ins now live in `github.com/yeqown/memcached/resolver` and `github.com/yeqown/memcached/picker`. Node addresses are defined by `resolver.Addr`. Custom resolvers implement `Resolve(ctx context.Context, target string) (resolver.ResolveResult, *time.Time, error)`. Return the full node list in `ResolveResult.Addrs` and a separate next resolve time. A nil time stops refreshing, including on error; transient errors should return a retry time. The default `resolver.NewStatic()` resolves once.

Custom pickers implement `picker.Picker` and are passed directly through `memcached.WithPicker(p)`. The Builder interface has been removed; built-in constructors such as `picker.NewCRC32HashPicker()` return a ready-to-use Picker. Hash functions are internal to `picker`; the standalone `hash` package has been removed. The client reuses the supplied picker across topology changes and supplies the current immutable address snapshot to each `Pick` call. Pickers must support concurrent calls and must not retain or modify the supplied addresses. Published addresses are normalized and sorted; upgrading can change existing key placement. Changing membership or switching hash strategies can cause cache misses, so applications need an origin fallback or cache warming.

## More

- [Usage guide: commands, routing, codecs, telemetry, and transport](./docs/usage.md)
- [Examples](./example/) and [Go API reference](https://pkg.go.dev/github.com/yeqown/memcached)
- [CLI installation and commands](./cmd/memcached-cli/README.md)
- [MC-COMPRESS flag format](./docs/MC-COMPRESS-SPEC-v1.0.md)
