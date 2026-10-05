# Usage guide

The [README](../README.md) covers installation and a first request. This guide collects the rest of the client API and its operational limits. All cache operations accept a `context.Context`; `Close` does not.

## Commands

| Task | Methods |
| --- | --- |
| Store values | `Set`, `Add`, `Replace`, `Append`, `Prepend`, `Cas` |
| Read values | `Get`, `Gets` (includes CAS), `GetAndTouch`, `GetAndTouches` |
| Manage values | `Delete`, `Incr`, `Decr`, `Touch`, `FlushAll` |
| Inspect a server | `Version`, `Stats` |
| Meta text protocol | `MetaGet`, `MetaSet`, `MetaDelete`, `MetaArithmetic`, `MetaDebug`, `MetaNoOp` |

`Set`, `Add`, `Replace`, `Append`, `Prepend`, and `Cas` accept a value, a `uint32` flags field, and an expiration as `time.Duration`. Use zero duration for no expiration. A missing `Get` returns `ErrNotFound`; use `errors.Is` to distinguish it from other failures. `Gets(ctx, key)` returns an item's CAS value for `Cas`.

The meta methods take options named `MetaGetFlag...`, `MetaSetFlag...`, `MetaDeleteFlag...`, or `MetaArithmeticFlag...`. They require a server with meta text protocol support. For example:

```go
item, err := client.MetaGet(ctx, []byte("article:1"),
	memcached.MetaGetFlagReturnValue(),
	memcached.MetaGetFlagReturnCAS(),
	memcached.MetaGetFlagReturnTTL(),
)
```

Other meta options cover client flags, size, opaque tokens, invalidation, and recache-related request flags. `MetaItem` does not expose W/X/Z response markers, so a complete stale/recache workflow is not available through the typed API. See [the meta example](../example/meta.go) and [Go API reference](https://pkg.go.dev/github.com/yeqown/memcached) for the option list.

## Multiple servers and connection pools

Pass a comma-separated list of addresses to `New`. Routing uses CRC32 by default. Murmur3 and rendezvous hashing are available through `WithPickBuilder`; `WithResolver` and `WithPickBuilder` also accept custom implementations.

```go
client, err := memcached.New(
	"cache-1:11211,cache-2:11211",
	memcached.WithPickBuilder(memcached.NewRendezvousHashPickBuilder(0)),
	memcached.WithMaxConns(100),
	memcached.WithMaxIdleConns(10),
	memcached.WithMaxLifetime(time.Hour),
	memcached.WithMaxIdleTimeout(5*time.Minute),
	memcached.WithDialTimeout(3*time.Second),
	memcached.WithReadTimeout(5*time.Second),
	memcached.WithWriteTimeout(5*time.Second),
)
```

Pools are maintained per address. The address list is resolved when `New` is called; this client does not automatically discover nodes or fail over to another node. Changing the server list or hash strategy can change key placement. `FlushAll` reaches every configured node, while `Version` and `Stats` report one selected node.

Unlike `gomemcache.GetMulti`, this client's `Gets` and `GetAndTouches` do not split keys by node. With multiple servers, use them only for keys known to map to the same node, or call `Get`/`GetAndTouch` separately for each key.

## Value codecs and compression

`WithCodec` installs a `Codec` that transforms values and flags when storing or retrieving data. The API accepts `[]byte`, so applications must serialize Go values before calling it. A custom `Codec` can transform those bytes and flags. The built-in compression codec follows the [MC-COMPRESS flag format](./MC-COMPRESS-SPEC-v1.0.md):

```go
compressionCodec, err := memcodec.NewCompressCodec(
	memcodec.CompressionAlgorithmZstd, 1024, 3,
)
if err != nil {
	return err
}
client, err := memcached.New("localhost:11211", memcached.WithCodec(compressionCodec))
```

Import `memcodec` from `github.com/yeqown/memcached/codec`. Available algorithms are `CompressionAlgorithmNone`, `CompressionAlgorithmDeflate`, `CompressionAlgorithmLZ4`, `CompressionAlgorithmSnappy`, and `CompressionAlgorithmZstd`. The second constructor argument is the minimum value size in bytes, and the third is the algorithm-specific compression level. Values below the threshold or not reduced by compression are stored uncompressed.

With this codec installed, reads decode recognized MC-COMPRESS values and return application flags. Application flags must fit in 16 bits. The compression codec rejects `Append` and `Prepend` and the equivalent meta set modes, which cannot preserve compressed-value semantics. Install the same codec on every client that must read those compressed values.

## Telemetry

Pass OpenTelemetry providers to `WithTelemetry` to collect request spans and the `memcached.operation.calls`, `memcached.operation.errors`, and `memcached.operation.duration` metrics. Telemetry is off unless providers are configured.

```go
client, err := memcached.New("localhost:11211",
	memcached.WithTelemetry(
		telemetry.WithTracerProvider(tracerProvider),
		telemetry.WithMeterProvider(meterProvider),
	),
)
```

Import `telemetry` from `github.com/yeqown/memcached/telemetry`. The [OpenTelemetry example](../example/otel/main.go) shows provider setup and exporters.

## Transport and other options

- TCP is the default: `localhost:11211`.
- Unix socket: `unix:///path/to/memcached.sock`.
- UDP: `udp://localhost:11211` with `WithUDPEnabled()`. The server must also enable UDP; this legacy transport is disabled in many deployments. `WithUDPEnabled` applies to every configured connection, so avoid mixing UDP and TCP addresses in one client.
- `WithNoReply()` skips acknowledgments for supported write commands; use it only when the application can tolerate missing server-side errors. `Incr` and `Decr` return zero in this mode.
- `WithSASL(username, password)` remains for legacy binary-protocol authentication and is deprecated because the binary protocol is deprecated.

The repository also contains an [interactive CLI](../cmd/memcached-cli/README.md) and a [Wails desktop GUI](../gui/README.md).
