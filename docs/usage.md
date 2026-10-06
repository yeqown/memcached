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

Pass a comma-separated list of addresses to `New`. Routing uses CRC32 by default. The `picker` package provides CRC32, Murmur3, rendezvous hashing, and stable rendezvous hashing. `WithResolver` and `WithPicker` accept the interfaces from the independent `resolver` and `picker` packages.

```go
client, err := memcached.New(
	"cache-1:11211,cache-2:11211",
	memcached.WithPicker(picker.NewStableRendezvousHashPicker(0)),
	memcached.WithMaxConns(100),
	memcached.WithMaxIdleConns(10),
	memcached.WithMaxLifetime(time.Hour),
	memcached.WithMaxIdleTimeout(5*time.Minute),
	memcached.WithDialTimeout(3*time.Second),
	memcached.WithReadTimeout(5*time.Second),
	memcached.WithWriteTimeout(5*time.Second),
)
```

Import `picker` from `github.com/yeqown/memcached/picker`. Its constructors are `NewCRC32HashPicker()`, `NewMurmur3HashPicker(seed)`, `NewRendezvousHashPicker(seed)`, and `NewStableRendezvousHashPicker(seed)`. Each returns a ready-to-use `picker.Picker` for `memcached.WithPicker`.

Pools are maintained per normalized network/address pair. Unchanged nodes reuse their pools, even when a resolver returns fresh address objects. Newly discovered nodes connect on demand. Client routes over the current address list and binds each address to an instance owning its pool. Each request retains its selected instance until its connection is returned; `FlushAll` retains all original target instances before launching child requests. Removed instances stop accepting new requests, but retained requests can still borrow connections, including waiting for capacity or dialing. Their pools close after the last retained request finishes. A node that rejoins gets a new instance and pool while older requests finish on the dropped instance. `Version` and `Stats` report one selected node.

`WithMaxConns` limits each pool, including in-progress dials. Waiting for capacity respects the request context. `Close` cancels discovery, waits for its task to exit, closes idle connections, and prevents new requests with `ErrClientClosed`. Borrowed connections can finish their current request and close on return. Repeated and concurrent `Close` calls return the same result.

Address lists are normalized and sorted by network/address before publication. This makes routing independent of discovery response order, but can change key placement when upgrading from caller-supplied ordering. Membership changes remap keys: CRC32 and Murmur3 use modulo routing, while `NewStableRendezvousHashPicker(seed)` scores only network/address/key and leaves unaffected keys on their nodes. `NewRendezvousHashPicker` retains the legacy scoring rules, which include `Priority`. Discovery does not move cached data or replay failed data commands; use an origin fallback or cache warming for misses after a topology or hash change.

Unlike `gomemcache.GetMulti`, this client's `Gets` and `GetAndTouches` do not split keys by node. They route the whole command by its first key. With multiple servers, use them only for keys known to map to the same node, or call `Get`/`GetAndTouch` separately for each key.

## Resolvers and automatic discovery

The `resolver` package contains `NewStatic()`, the default resolver that parses a comma-separated list once, and `NewAutoDiscovery(refreshInterval)`, which implements the shared AWS ElastiCache Memcached and Google Memorystore Memcached discovery protocol. Custom resolvers can implement DNS/SRV, a registry, a file, or another address format through the same interface:

```go
// In package resolver:
type Resolver interface {
	Resolve(ctx context.Context, target string) (
		result ResolveResult,
		nextResolveAt *time.Time,
		err error,
	)
}

type ResolveResult struct {
	Addrs   []*Addr
	Version string // optional source information
}

// In package picker:
type Picker interface {
	Pick(addrs []*resolver.Addr, cmd, key []byte) (*resolver.Addr, error)
}
```

Node addresses are `resolver.Addr`, created with `resolver.NewAddr(network, address, priority)` from `github.com/yeqown/memcached/resolver`. `Addr.Add` sets metadata before publication; `Clone` copies the map, and `Equal` compares only `Network` and `Address`.

`Resolve` must honor its context and return a complete nonempty list of nodes. Client always passes the original target, which can differ from data node addresses. Initial errors or invalid results fail `New`; later errors, empty results, conflicting duplicates, and invalid nodes preserve the last successful topology. Supported node networks are `tcp`, `tcp4`, `tcp6`, `udp`, `udp4`, `udp6`, and `unix`. Address validation checks syntax; DNS lookup happens when connecting or inside a custom resolver.

The resolver controls TTLs, refresh intervals, jitter, and backoff by returning an absolute `nextResolveAt`. A nil time stops refreshing and keeps serving with the current topology, **even when the resolve attempt returns an error**. A transient error should therefore include a retry time. A past time runs the next attempt immediately; avoid repeatedly returning expired times. Client follows each returned schedule even when the topology is unchanged or the result is invalid. It runs one serial resolve/apply task and bounds every attempt with `WithResolveTimeout`, default five seconds. A resolver that ignores cancellation can delay `Close`.

`Version` is optional. Client applies the returned addresses without comparing version strings or using them to skip node changes. Source-specific rules such as numeric ordering or rejecting version rollback belong to the resolver. Identical membership, priority, and metadata preserve the active addresses and instances; changing only a version does not advance the topology generation.

Client copies address fields, the metadata map, and the returned time value. Metadata values must be immutable, and the resolver must not modify returned objects concurrently while Client accepts them. Stateful resolvers should be dedicated to one client or synchronize their shared state. The client retains the supplied picker across topology changes. `Pick` must support concurrent calls, return a node from its supplied address list, and avoid retaining or modifying that list. Selection and instance retention happen together before discovery can remove the selected node. A picker can be shared by multiple clients; any mutable state or custom hash function must support concurrent calls too.

To migrate custom implementations, import `resolver.Resolver`, `resolver.ResolveResult`, `resolver.Addr`, and `picker.Picker` instead of the old root-package types. Change `Resolve(addr string) ([]*Addr, error)` to the context-aware signature above and wrap addresses in `resolver.ResolveResult`. Return nil for a static resolver's next time. Remove custom Builder implementations, construct a Picker directly, and pass it to `memcached.WithPicker`. Route using the addresses supplied to each `Pick` call. Move built-in constructor calls to the `picker` package; CRC32 uses `picker.NewCRC32HashPicker()`. The standalone `hash` package has been removed; its Murmur3 digest is now internal to `picker`. Select a built-in Picker, or supply a custom hash function through `picker.NewRendezvousHashPickerWithHash`.

### AWS and Google automatic discovery

Pass the AWS ElastiCache **configuration endpoint** or Google Memorystore **discovery endpoint**, including its port, as the target:

```go
client, err := memcached.New(
	"discovery-endpoint:11211",
	memcached.WithResolver(resolver.NewAutoDiscovery(time.Minute)),
	memcached.WithResolveTimeout(5*time.Second),
	memcached.WithPicker(picker.NewStableRendezvousHashPicker(0)),
)
```

Import `resolver` from `github.com/yeqown/memcached/resolver`. The same built-in resolver works with both services; it does not call cloud management APIs or require cloud SDKs. The endpoint must be reachable from the application's network.

Each attempt sends only `config get cluster\r\n` and parses the `CONFIG cluster 0 <bytes>` response line by line, following [Google's reference parser](https://github.com/google/gomemcache/blob/master/memcache/cluster_config_parser.go). It accepts LF or CRLF line endings and skips blank lines, validates the numeric version and `hostname|ip|port` entries, and stops at `END` before closing the discovery connection. IP addresses take precedence; an empty IP falls back to the hostname, supporting AWS nodes that omit their IP. The advertised size is checked against a 1 MiB limit, and reads are bounded independently of that size. The resolver rejects incomplete or invalid replies and version rollback for the same endpoint, preserving Client's last successful topology. If a cluster is recreated with a lower config version at the same endpoint, create a new resolver/client to reset the observed version.

`NewAutoDiscovery` returns a next resolve time after **every** attempt, including errors. Non-positive refresh intervals select one minute. `WithResolveTimeout` bounds the TCP connect, write and read; `Close` cancels an active attempt. It supports `host:port`, `tcp://host:port`, `tcp4://host:port`, and `tcp6://[host]:port` targets. All discovered data nodes use TCP and a stable priority of zero. The legacy `get AmazonElastiCache:cluster` protocol for older AWS engines is not implemented.

Protocol references: [AWS Auto Discovery](https://docs.aws.amazon.com/AmazonElastiCache/latest/dg/AutoDiscovery.AddingToYourClientLibrary.html) and [Google's open Memcache auto-discovery protocol](https://docs.google.com/document/d/15V9tKuffWrcCVwDZRmRBOV1SDcuo6P8u05dddwZYxCo/edit).


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

With a meter provider configured, discovery also records `memcached.discovery.resolve.calls`, `.errors`, `.duration` (seconds), and `.last_success` (Unix seconds). Duration includes result validation and application. Failures preserve the last success time. Topology instruments are `memcached.topology.nodes`, `.generation`, `.nodes_added`, and `.nodes_removed`; unchanged results do not emit a topology change. Each discovery/topology measurement has a stable generated `memcached.client.id` attribute, so multiple clients using one provider keep separate gauge values. Close records zero current nodes and the final removals. Source versions and complete node lists are not metric attributes.

## Transport and other options

- TCP is the default: `localhost:11211`.
- Unix socket: `unix:///path/to/memcached.sock`.
- UDP: `udp://localhost:11211` with `WithUDPEnabled()`. The server must also enable UDP; this legacy transport is disabled in many deployments. `WithUDPEnabled` applies to every configured connection, so avoid mixing UDP and TCP addresses in one client.
- `WithNoReply()` skips acknowledgments for supported write commands; use it only when the application can tolerate missing server-side errors. `Incr` and `Decr` return zero in this mode.
- `WithSASL(username, password)` remains for legacy binary-protocol authentication and is deprecated because the binary protocol is deprecated.

The repository also contains an [interactive CLI](../cmd/memcached-cli/README.md) and a [Wails desktop GUI](../cmd/gui/README.md).
