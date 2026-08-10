package main

import (
	"context"
	"time"

	"github.com/yeqown/memcached"
)

func main() {
	addrs := "localhost:11211"
	client, err := memcached.New(
		addrs,
		memcached.WithMaxConns(10),
		memcached.WithMaxIdleConns(5),
		memcached.WithMaxLifetime(24*time.Hour),
		memcached.WithMaxIdleTimeout(10*time.Minute),
	)
	if err != nil {
		panic(err)
	}

	// flush all
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err = client.FlushAll(ctx); err != nil {
		panic(err)
	}
}
