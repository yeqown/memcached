package main

import (
	"github.com/yeqown/memcached"
)

func main() {
	addrs := "localhost:11211"
	client, err := memcached.New(addrs)
	if err != nil {
		panic(err)
	}

	version, err := client.Version()
	if err != nil {
		panic(err)
	}
	println("Version: ", version)

	if err = client.Set("key", "value", 0, 0); err != nil {
		panic(err)
	}
	if err = client.Set("key2", "value2", 0, 0); err != nil {
		panic(err)
	}

	items, err := client.Gets("key", "key2")
	if err != nil {
		panic(err)
	}

	for _, item := range items {
		println("key: ", item.Key, " value: ", string(item.Value))
	}
}
