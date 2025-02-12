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
	client, err := memcached.New(addrs)
	if err != nil {
		panic(err)
	}

	// Set a key with the value "bar" and the default options.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	item, err := client.MetaSet(ctx, "foo", []byte("bar"))
	if err != nil {
		panic(err)
	}
	fmt.Printf("Meta set, key=%s, value=%+v\n", item.Key, item)

	// Get the value of the key "foo".
	item2, err := client.MetaGet(ctx, "foo")
	if err != nil {
		panic(err)
	}
	fmt.Printf("Meta get, key=%s, value=%+v\n", item.Key, item2)
}
