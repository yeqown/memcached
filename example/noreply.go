package main

import (
	"context"
	"time"

	"github.com/yeqown/memcached"
)

func main() {
	addrs := "localhost:11211"
	client, err := memcached.New(addrs, memcached.WithNoReply())
	if err != nil {
		panic(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Second)
	defer cancel()

	err = client.Set(ctx, "noReply", "noReply", 0, 0)
	if err != nil {
		panic(err)
	}

	// no reply
	err = client.Delete(ctx, "noReply")
	if err != nil {
		panic(err)
	}
}
