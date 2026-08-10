package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/yeqown/memcached"
)

func main() {
	addrs := "udp://localhost:11211"
	client, err := memcached.New(addrs, memcached.WithUDPEnabled())
	if err != nil {
		panic(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Second)
	defer cancel()

	version, err := client.Version(ctx)
	if err != nil {
		panic(err)
	}
	fmt.Println("Version:", version)

	key := "key:udp"
	value := "This is a value for key:udp"

	// get first
	_, err = client.Get(ctx, key)
	if err != nil {
		if !errors.Is(err, memcached.ErrNotFound) {
			panic(err)
		}

		fmt.Println("'key' not found")
	}

	// set
	if err = client.Set(ctx, key, []byte(value), 0, 100*time.Second); err != nil {
		panic(err)
	}

	// get again
	item, err := client.Get(ctx, key)
	if err != nil {
		panic(err)
	}

	fmt.Println("key:", item.Key, "value:", string(item.Value))
}
