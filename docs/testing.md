# Testing

The main module's default suite needs no Memcached daemon:

```sh
go test -race ./...
# or
make test
```

Most tests use fixed protocol responses and in-memory connections. Transport
tests open temporary local TCP, UDP or Unix sockets to check deadlines and
cancellation. They need permission to listen on loopback, but no external
service. Codec, picker, resolver and telemetry tests run in the same suite.

Resolver parsing, generation checks and cache behavior use fixed readers and
connections. Cancellation uses `net.Pipe`; TCP tests check dialing,
connection reuse, reconnection after a failed exchange and repeated shutdown.

For coverage, run `make coverage`. CI collects coverage from the default suite;
daemon compatibility runs in a separate job.

## Real Memcached compatibility

`TestMemcachedIntegration` in `client_test.go` checks classic storage, retrieval,
CAS, arithmetic and deletion, meta storage and
retrieval, compression flags and application transparency, and concurrent
MetaSet/Get/Touch behavior from issue #18.

Start a dedicated Memcached 1.6.37 instance and pass its single TCP endpoint
explicitly. The test skips if `MEMCACHED_TEST_ADDR` is absent. The
`make test-integration` target requires it. Each run uses unique keys and deletes
only those keys during cleanup.

```sh
make docker-up
MEMCACHED_TEST_ADDR=127.0.0.1:11211 make test-integration
make docker-down
```

To select the test directly or use a different dedicated endpoint:

```sh
MEMCACHED_TEST_ADDR=127.0.0.1:11212 \
  go test -race -run '^TestMemcachedIntegration$' -count=1 .
```

The Makefile and CI pin the same daemon version. Running only
`TestMemcachedIntegration` keeps compatibility checks independent of the default
suite, and `-count=1` ensures Go contacts the daemon on each run.

## Other modules and benchmarks

The root `./...` pattern does not enter nested Go modules. Run their checks from
their own directories:

```sh
(cd cmd/gui && go test -race ./service/...)
(cd cmd/memcached-cli && go test ./...)
(cd benchmark && go test -run '^$' -bench '^$' ./...)
```

The last command compiles the benchmark module without contacting a daemon.
The actual benchmarks use `localhost:11211` and fixed keys, so run them against
a dedicated daemon:

```sh
make docker-up
(cd benchmark && go test -run '^$' -bench '^BenchmarkYeqownMemcached$' -benchmem -count=5)
make docker-down
```

The checked-in `go.work` connects these modules to the current client checkout.
Keep workspace mode enabled when checking working-tree changes. With
`GOWORK=off`, nested modules use the released client version in their `go.mod`;
the GUI additionally needs the current checkout's codec package. See
[benchmark/README.md](../benchmark/README.md) for comparison and profiling
commands.

## Known compatibility gaps

The compatibility suite currently uses values without embedded CRLF. During
this refactor, a daemon check exposed an existing meta response framing issue:
such values are truncated and leave unread bytes on pooled connections.
`MetaNoOp` also currently sends `noop` instead of `mn`. These production fixes
are separate from the test cleanup; the suite does not assert those behaviors
as valid protocol compatibility.
