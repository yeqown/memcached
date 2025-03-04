package main

import (
	"context"
	"errors"
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
	println("Version: ", version)

	key := "key:udp"
	value := "This is a value for key:udp"

	// get first
	item, err := client.Get(ctx, key)
	if err != nil {
		if !errors.Is(err, memcached.ErrNotFound) {
			panic(err)
		}

		println("'key' not found")
	}

	// set
	if err = client.Set(ctx, key, []byte(value), 0, 100); err != nil {
		panic(err)
	}

	// get again
	item, err = client.Get(ctx, key)
	if err != nil {
		panic(err)
	}

	println("key: ", item.Key, " value: ", string(item.Value))
}
