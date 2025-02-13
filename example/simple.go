package main

import (
	"context"
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

	version, err := client.Version(ctx)
	if err != nil {
		panic(err)
	}
	println("Version: ", version)

	if err = client.Set(ctx, "key", []byte("value"), 0, 0); err != nil {
		panic(err)
	}
	if err = client.Set(ctx, "key2", []byte("value2"), 0, 0); err != nil {
		panic(err)
	}

	items, err := client.Gets(ctx, "key", "key2")
	if err != nil {
		panic(err)
	}

	for _, item := range items {
		println("key: ", item.Key, " value: ", string(item.Value))
	}
}
