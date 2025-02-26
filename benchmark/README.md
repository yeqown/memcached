## Benchmark differences

Comparing following three implementations:

- [yeqown/memcached](https://github.com/yeqown/memcahced)
- [bradfitz/gomemcache](https://github.com/bradfitz/gomemcache)
- <del>[rainy/memcache](github.com/rainycape/memcache)</del>

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

**Results:**

```plain
# bradfitz
BenchmarkBradfitzGomemcache-8   	   30140	     41691 ns/op	     256 B/op	      12 allocs/op
BenchmarkBradfitzGomemcache-8   	   29876	     40228 ns/op	     256 B/op	      12 allocs/op
BenchmarkBradfitzGomemcache-8   	   27021	     42845 ns/op	     256 B/op	      12 allocs/op
BenchmarkBradfitzGomemcache-8   	   30134	     40191 ns/op	     256 B/op	      12 allocs/op
BenchmarkBradfitzGomemcache-8   	   29623	     41470 ns/op	     256 B/op	      12 allocs/op
BenchmarkBradfitzGomemcache-8   	   29869	     39905 ns/op	     256 B/op	      12 allocs/op
BenchmarkBradfitzGomemcache-8   	   30460	     39898 ns/op	     256 B/op	      12 allocs/op
BenchmarkBradfitzGomemcache-8   	   28624	     41170 ns/op	     256 B/op	      12 allocs/op
BenchmarkBradfitzGomemcache-8   	   30502	     40170 ns/op	     256 B/op	      12 allocs/op
BenchmarkBradfitzGomemcache-8   	   29349	     40166 ns/op	     256 B/op	      12 allocs/op

# yeqown
BenchmarkYeqownMemcached-8   	   25741	     45721 ns/op	     329 B/op	      13 allocs/op
BenchmarkYeqownMemcached-8   	   26772	     46165 ns/op	     329 B/op	      13 allocs/op
BenchmarkYeqownMemcached-8   	   26264	     45671 ns/op	     329 B/op	      13 allocs/op
BenchmarkYeqownMemcached-8   	   26380	     45786 ns/op	     329 B/op	      13 allocs/op
BenchmarkYeqownMemcached-8   	   26779	     45458 ns/op	     329 B/op	      13 allocs/op
BenchmarkYeqownMemcached-8   	   26616	     45912 ns/op	     329 B/op	      13 allocs/op
BenchmarkYeqownMemcached-8   	   24794	     47299 ns/op	     329 B/op	      13 allocs/op
BenchmarkYeqownMemcached-8   	   26280	     45724 ns/op	     329 B/op	      13 allocs/op
BenchmarkYeqownMemcached-8   	   26390	     44376 ns/op	     329 B/op	      13 allocs/op
BenchmarkYeqownMemcached-8   	   25969	     44500 ns/op	     329 B/op	      13 allocs/op
```

2. concurrency benchmark

```bash
mkdir -p results
go test -bench=^BenchmarkYeqownMemcachedConcurrent$ -count=10 -benchmem > results/yeqown_concurrent.txt
go test -bench=^BenchmarkBradfitzGomemcacheConcurrent$ -count=10 -benchmem > results/bradfitz_concurrent.txt

benchstat results/bradfitz_concurrent.txt results/yeqown_concurrent.txt
```

**Results:**

```plain
# bradfitz
BenchmarkBradfitzGomemcacheConcurrent-8   	   59113	     24163 ns/op	     263 B/op	      12 allocs/op
BenchmarkBradfitzGomemcacheConcurrent-8   	   55773	     22248 ns/op	     263 B/op	      12 allocs/op
BenchmarkBradfitzGomemcacheConcurrent-8   	   58000	     22118 ns/op	     262 B/op	      12 allocs/op
BenchmarkBradfitzGomemcacheConcurrent-8   	   41526	     40982 ns/op	     264 B/op	      12 allocs/op
BenchmarkBradfitzGomemcacheConcurrent-8   	   58578	     21371 ns/op	     264 B/op	      12 allocs/op
BenchmarkBradfitzGomemcacheConcurrent-8   	   50187	     21724 ns/op	     263 B/op	      12 allocs/op
BenchmarkBradfitzGomemcacheConcurrent-8   	   52758	     32467 ns/op	     262 B/op	      12 allocs/op
BenchmarkBradfitzGomemcacheConcurrent-8   	   56595	     25040 ns/op	     263 B/op	      12 allocs/op
BenchmarkBradfitzGomemcacheConcurrent-8   	   54022	     21743 ns/op	     263 B/op	      12 allocs/op
BenchmarkBradfitzGomemcacheConcurrent-8   	   58392	     21872 ns/op	     263 B/op	      12 allocs/op

# yeqown
BenchmarkYeqownMemcachedConcurrent-8   	   55108	     19990 ns/op	     330 B/op	      13 allocs/op
BenchmarkYeqownMemcachedConcurrent-8   	   61858	     19800 ns/op	     330 B/op	      13 allocs/op
BenchmarkYeqownMemcachedConcurrent-8   	   59310	     19862 ns/op	     330 B/op	      13 allocs/op
BenchmarkYeqownMemcachedConcurrent-8   	   57694	     20845 ns/op	     330 B/op	      13 allocs/op
BenchmarkYeqownMemcachedConcurrent-8   	   61292	     19947 ns/op	     330 B/op	      13 allocs/op
BenchmarkYeqownMemcachedConcurrent-8   	   61142	     20008 ns/op	     330 B/op	      13 allocs/op
BenchmarkYeqownMemcachedConcurrent-8   	   59674	     20191 ns/op	     330 B/op	      13 allocs/op
BenchmarkYeqownMemcachedConcurrent-8   	   49004	     31806 ns/op	     330 B/op	      13 allocs/op
BenchmarkYeqownMemcachedConcurrent-8   	   53425	     19917 ns/op	     330 B/op	      13 allocs/op
BenchmarkYeqownMemcachedConcurrent-8   	   57700	     23123 ns/op	     330 B/op	      13 allocs/op
```

3. benchmark profile

```bash
mkdir -p results
go test -bench=^BenchmarkYeqownMemcachedConcurrent$ -count=10 -benchmem -memprofile=results/mem.pprof -cpuprofile=results/cpu.pprof

# view profile
go tool pprof -http=:8080 results/cpu.pprof
go tool pprof -http=:8081 results/mem.pprof
```
