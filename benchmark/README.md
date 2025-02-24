## Benchmark differences

Comparing following three implementations:

- [yeqown/memcached](https://github.com/yeqown/memcahced)
- [bradfitz/gomemcache](https://github.com/bradfitz/gomemcache)
- [rainy/memcache](github.com/rainycape/memcache)

## How to run

> **NOTE**: Make sure you have installed `benchstat`.
> And we can use `go build -gcflags="-m" ./...` to check the escape analysis.

```bash
# install benchcmp
go install golang.org/x/tools/cmd/benchcmp@latest
```

To run benchmark and analysis:


1. memory benchmark

```bash
# run benchmark
mkdir -p results

# run benchmark test and save result to file, 10 times
go test -bench=^BenchmarkYeqownMemcached$ -count=10 -benchmem >results/yeqown.txt
go test -bench=^BenchmarkBradfitzGomemcache$ -count=10 -benchmem >results/bradfitz.txt

# benchstat analysis
benchstat results/bradfitz.txt results/yeqown.txt
```

**Differences:**

```plain
TODO:

```

2. concurrency benchmark

```bash
mkdir -p results
go test -bench=^BenchmarkYeqownMemcachedConcurrent$ -count=10 -benchmem > results/yeqown_concurrent.txt
go test -bench=^BenchmarkBradfitzGomemcacheConcurrent$ -count=10 -benchmem > results/bradfitz_concurrent.txt

benchstat results/bradfitz_concurrent.txt results/yeqown_concurrent.txt
```

**Differences:**

```plain
TODO:
```

3. benchmark profile

```bash
mkdir -p results
go test -bench=^BenchmarkYeqownMemcachedConcurrent$ -count=10 -benchmem -memprofile=results/mem.pprof -cpuprofile=results/cpu.pprof

# view profile
go tool pprof -http=:8080 results/cpu.pprof
go tool pprof -http=:8081 results/mem.pprof
```
