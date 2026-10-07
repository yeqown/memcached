package benchmark

import (
	"context"
	"testing"
	"time"

	"github.com/bradfitz/gomemcache/memcache"
	rainycape "github.com/rainycape/memcache"
	"github.com/yeqown/memcached"
)

func Test_Yeqown(t *testing.T) {
	client, err := memcached.New("localhost:11211")
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	client.Set(context.Background(), testKey, testValue, 0, 0)
	item, err := client.Get(context.Background(), testKey)
	if err != nil {
		t.Fatal(err)
	}
	if string(item.Value) != string(testValue) {
		t.Fatalf("expect %s, got %s", string(testValue), string(item.Value))
	}
}

func Test_Rainycape(t *testing.T) {
	t.Skipf("It's a binary package, not support test")

	client, err := rainycape.New("127.0.0.1:11211")
	if err != nil {
		t.Fatal(err)
	}
	client.Set(&rainycape.Item{
		Key:   testKey,
		Value: testValue,
	})
	item, err := client.Get(testKey)
	if err != nil {
		t.Fatal(err)
	}
	if string(item.Value) != string(testValue) {
		t.Fatalf("expect %s, got %s", string(testValue), string(item.Value))
	}
}

func Test_Bradfitz(t *testing.T) {
	client := memcache.New("localhost:11211")
	client.Timeout = 10 * time.Second
	client.MaxIdleConns = 10

	if err := client.Ping(); err != nil {
		t.Fatalf("ping failed: %v", err)
	}

	if err := client.Set(&memcache.Item{
		Key:   testKey,
		Value: testValue,
	}); err != nil {
		t.Fatalf("set failed: %v", err)
	}

	item, err := client.Get(testKey)
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	if string(item.Value) != string(testValue) {
		t.Fatalf("expect %s, got %s", string(testValue), string(item.Value))
	}
}

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
