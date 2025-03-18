package main

import (
	"context"
	"time"

	"github.com/yeqown/memcached"
)

// Deprecated:
// since SASL is supported over binary protocol, but binary protocol is deprecated.
func main() {
	addrs := "localhost:11211"
	client, err := memcached.New(addrs, memcached.WithSASL("memcached", "password123"))
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

	if err = client.Set(ctx, "key", []byte("value"), 123, 5*time.Second); err != nil {
		panic(err)
	}
}
