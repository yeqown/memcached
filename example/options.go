package main

import (
	"context"
	"fmt"
	"time"

	"github.com/yeqown/memcached"
)

func main() {
	addrs := "localhost:11211"
	// Create a new client with the default options.
	client, err := memcached.New(addrs,
		memcached.WithDialTimeout(time.Second*5),
		memcached.WithReadTimeout(100*time.Second),
		memcached.WithWriteTimeout(100*time.Second),
	)
	if err != nil {
		panic(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	key := "example:options"

	err = client.Set(ctx, key, []byte("value"), 0, 0)
	if err != nil {
		panic(err)
	}

	for i := 0; i < 100; i++ {
		item, err := client.Get(ctx, key)
		if err != nil {
			fmt.Printf("Get key=%s failed, err=%v\n", key, err)
			continue
		}

		fmt.Printf("got %+v\n", item)
		time.Sleep(200 * time.Millisecond)
	}
}
