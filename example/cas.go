package main

import (
	"context"
	"fmt"
	"time"

	"github.com/yeqown/memcached"
)

func main() {
	addrs := "localhost:11211"
	client, err := memcached.New(addrs)
	if err != nil {
		panic(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Second)
	defer cancel()

	key := "example:cas"

	// first set
	err = client.Set(ctx, key, "value2", 123, 10)
	if err != nil {
		panic(err)
	}
	// gets
	items, err := client.Gets(ctx, key)
	if err != nil {
		panic(err)
	}
	item := items[0]
	fmt.Printf("before cas, key: %s, value: %+v\n", item.Key, item)

	// cas
	err = client.Cas(ctx, key, "value2", 123, 10, item.CAS)
	if err != nil {
		panic(err)
	}

	// cas again, this should fail:
	err = client.Cas(ctx, key, "value3", 123, 10, item.CAS)
	if err != nil {
		fmt.Printf("cas failed: %v\n", err)
	}

	// get again
	items, err = client.Gets(ctx, key)
	if err != nil {
		panic(err)
	}
	item = items[0]

	fmt.Printf("key: %s, value: %+v\n", item.Key, item)
}
