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
