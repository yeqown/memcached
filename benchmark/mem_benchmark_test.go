package benchmark

import (
	"context"
	"testing"
	"time"

	"github.com/bradfitz/gomemcache/memcache"
	rainycape "github.com/rainycape/memcache"
	"github.com/yeqown/memcached"
)

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
	defer client.Close()

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

func BenchmarkRainycapeMemcache(b *testing.B) {
	b.Skipf("It's a binary package, not support benchmark.")

	client, err := rainycape.New("localhost:11211")
	if err != nil {
		b.Fatal(err)
	}
	defer client.Close()
	item := &rainycape.Item{
		Key:   testKey,
		Value: testValue,
	}

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
