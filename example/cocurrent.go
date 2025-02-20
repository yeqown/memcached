package main

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/yeqown/memcached"
)

// FIXME(@yeqown): concurrent set and get would cause failure
func main() {
	addrs := "localhost:11211"
	client, err := memcached.New(addrs)
	if err != nil {
		panic(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	limits := 1000

	wg := sync.WaitGroup{}
	// start 10 goroutine to set
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			defer func() { fmt.Printf("routine(%d) done\n", i) }()

			for counter := 0; counter < limits; counter++ {
				key := "example:cas"
				// set
				err = client.Set(ctx, key, []byte("value2"), 123, 10)
				if err != nil {
					panic("write: " + err.Error())
				}
				// get
				item, err := client.Get(ctx, key)
				if err != nil {
					panic("read: " + err.Error())
				}
				fmt.Printf("get, key: %s, value: %+v\n", item.Key, item)
			}
		}(i)
	}

	wg.Wait()
	fmt.Println("done")
}
