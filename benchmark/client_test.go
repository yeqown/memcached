package benchmark

import (
	"context"
	"testing"
	"time"

	"github.com/bradfitz/gomemcache/memcache"
	"github.com/yeqown/memcached"
)

func Test_Yeqown(t *testing.T) {
	client, err := memcached.New("localhost:11211")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := client.Close(); err != nil {
			t.Errorf("close client: %v", err)
		}
	})
	if err := client.Set(context.Background(), testKey, testValue, 0, 0); err != nil {
		t.Fatalf("set failed: %v", err)
	}
	item, err := client.Get(context.Background(), testKey)
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
