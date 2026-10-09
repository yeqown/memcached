package benchmark

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/bradfitz/gomemcache/memcache"
	"github.com/yeqown/memcached"
)

func BenchmarkYeqownMemcachedConcurrent(b *testing.B) {
	client, err := memcached.New("localhost:11211")
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() {
		if err := client.Close(); err != nil {
			b.Error(err)
		}
	})

	ctx := context.Background()

	var firstErr error
	var errorOnce sync.Once
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		defer func() {
			// Exhaust the iteration counter on error so RunParallel can return.
			for pb.Next() {
			}
		}()
		for pb.Next() {
			if err := client.Set(ctx, testKey, testValue, 0, 0); err != nil {
				errorOnce.Do(func() { firstErr = err })
				return
			}
			if _, err := client.Get(ctx, testKey); err != nil {
				errorOnce.Do(func() { firstErr = err })
				return
			}
		}
	})
	if firstErr != nil {
		b.Fatal(firstErr)
	}
}

func BenchmarkBradfitzGomemcacheConcurrent(b *testing.B) {
	client := memcache.New("localhost:11211")
	item := &memcache.Item{
		Key:   testKey,
		Value: testValue,
	}

	var firstErr error
	var errorOnce sync.Once
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		defer func() {
			// Exhaust the iteration counter on error so RunParallel can return.
			for pb.Next() {
			}
		}()
		for pb.Next() {
			if err := client.Set(item); err != nil {
				errorOnce.Do(func() { firstErr = err })
				return
			}
			if _, err := client.Get(testKey); err != nil {
				errorOnce.Do(func() { firstErr = err })
				return
			}
		}
	})
	if firstErr != nil {
		b.Fatal(firstErr)
	}
}

var (
	testKey   = "test_key"
	testValue = []byte("test_value")
)

// BenchmarkYeqownMemcached
// go test -benchmem -run=^$ -bench ^BenchmarkYeqownMemcached$ -count 10 -benchmem
func BenchmarkYeqownMemcached(b *testing.B) {
	client, err := memcached.New("localhost:11211")
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() {
		if err := client.Close(); err != nil {
			b.Error(err)
		}
	})

	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := client.Set(ctx, testKey, testValue, 0, 0); err != nil {
			b.Fatal(err)
		}
		if _, err := client.Get(ctx, testKey); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkBradfitzGomemcache(b *testing.B) {
	client := memcache.New("localhost:11211")
	item := &memcache.Item{
		Key:   testKey,
		Value: testValue,
	}
	client.Timeout = 3 * time.Second
	client.MaxIdleConns = 10

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := client.Set(item); err != nil {
			b.Fatal(err)
		}
		if _, err := client.Get(testKey); err != nil {
			b.Fatal(err)
		}
	}
}
