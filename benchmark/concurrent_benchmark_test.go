package benchmark

import (
	"context"
	"testing"

	"github.com/bradfitz/gomemcache/memcache"
	"github.com/yeqown/memcached"
)

func BenchmarkYeqownMemcachedConcurrent(b *testing.B) {
	client, err := memcached.New("localhost:11211")
	if err != nil {
		b.Fatal(err)
	}
	defer client.Close()

	ctx := context.Background()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if err := client.Set(ctx, testKey, testValue, 0, 0); err != nil {
				b.Fatal(err)
			}
			if _, err := client.Get(ctx, testKey); err != nil {
				b.Fatal(err)
			}
		}
	})
}

func BenchmarkBradfitzGomemcacheConcurrent(b *testing.B) {
	client := memcache.New("localhost:11211")
	item := &memcache.Item{
		Key:   testKey,
		Value: testValue,
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if err := client.Set(item); err != nil {
				b.Fatal(err)
			}
			if _, err := client.Get(testKey); err != nil {
				b.Fatal(err)
			}
		}
	})
}
