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

	// set and get meta
	metaSetAndGet(client)

	// delete
	metaDelete(client)

	// arithmetic
	metaArithmetic(client)
}

func metaSetAndGet(client memcached.Client) {
	// Set a key with the value "bar" and the default options.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	fmt.Println("======== MetaSetAndGet example ========")

	key := []byte("example:meta:set")

	item, err := client.MetaSet(ctx, key, []byte("bar"),
		memcached.MetaSetFlagReturnCAS(),      // return CAS value
		memcached.MetaSetFlagBinaryKey(),      // binary encoded key
		memcached.MetaSetFlagClientFlags(123), // client flags
		memcached.MetaSetFlagTTL(100),         // expiry
		memcached.MetaSetFlagReturnKey(),      // return key
		memcached.MetaSetFlagOpaque(456),      // opaque
		memcached.MetaSetFlagReturnSize(),     // return size
	)
	if err != nil {
		panic(err)
	}
	_ = item
	// MetaItem{Key:example:meta:set Value: CAS:62 Flags:0 TTL:100 LastAccessedTime:0 Size:3 Opaque:456 HitBefore:false}
	fmt.Printf("Meta set, item=%+v\n", item)

	// Get the value of the key "foo".
	item2, err := client.MetaGet(ctx, key,
		memcached.MetaGetFlagBinaryKey(),              // binary encoded key
		memcached.MetaGetFlagReturnValue(),            // return value
		memcached.MetaGetFlagReturnCAS(),              // return CAS value
		memcached.MetaGetFlagReturnClientFlags(),      // return flags
		memcached.MetaGetFlagReturnTTL(),              // return expiry
		memcached.MetaGetFlagReturnKey(),              // return key
		memcached.MetaGetFlagReturnSize(),             // return size
		memcached.MetaGetFlagOpaque(789),              // return opaque
		memcached.MetaGetFlagReturnHitBefore(),        // return version
		memcached.MetaGetFlagReturnLastAccessedTime(), // return last accessed time
		memcached.MetaGetFlagNewCAS(item.CAS+1),       // return new CAS value
		memcached.MetaGetFlagUpdateRemainingTTL(200),  // update remaining TTL
	)
	if err != nil {
		panic(err)
	}
	_ = item2
	// MetaItem{Key:example:meta:set Value:bar CAS:62 Flags:123 TTL:100 LastAccessedTime:0 Size:3 Opaque:789 HitBefore:false}
	fmt.Printf("Meta get1, item=%+v\n", item2)

	// wait for the key last accessed time to be updated
	time.Sleep(2 * time.Second)

	item3, err := client.MetaGet(ctx, key,
		memcached.MetaGetFlagBinaryKey(),              // binary encoded key
		memcached.MetaGetFlagReturnValue(),            // return value
		memcached.MetaGetFlagReturnCAS(),              // return CAS value
		memcached.MetaGetFlagReturnClientFlags(),      // return flags
		memcached.MetaGetFlagReturnTTL(),              // return expiry
		memcached.MetaGetFlagReturnKey(),              // return key
		memcached.MetaGetFlagReturnSize(),             // return size
		memcached.MetaGetFlagOpaque(789),              // return opaque
		memcached.MetaGetFlagReturnHitBefore(),        // return version
		memcached.MetaGetFlagReturnLastAccessedTime(), // return last accessed time
		memcached.MetaGetFlagNewCAS(item.CAS+1),       // return new CAS value
		memcached.MetaGetFlagUpdateRemainingTTL(200),  // update remaining TTL
	)
	if err != nil {
		panic(err)
	}
	_ = item3
	// MetaItem{Key:example:meta:set Value:bar CAS:62 Flags:123 TTL:198 LastAccessedTime:2 Size:3 Opaque:789 HitBefore:true}
	fmt.Printf("Meta get2, item=%+v\n", item3)
}

func metaDelete(client memcached.Client) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	fmt.Println("======== MetaDelete example ========")

	key := []byte("example:meta:delete")

	// set a key
	item, err := client.MetaSet(ctx, key, []byte("bar"),
		memcached.MetaSetFlagBinaryKey(),
		memcached.MetaSetFlagReturnCAS(),
		memcached.MetaSetFlagTTL(100),
	)
	if err != nil {
		panic(err)
	}

	//  MetaItem{Key:example:meta:delete Value: CAS:55 Flags:0 TTL:0 LastAccessedTime:0 Size:0 Opaque:0 HitBefore:false}
	fmt.Printf("set item: %+v\n", item)

	// Delete the key "foo" 's value
	item2, err := client.MetaDelete(ctx, key,
		memcached.MetaDeleteFlagBinaryKey(),          // binary encoded key
		memcached.MetaDeleteFlagCompareCAS(item.CAS), // compare CAS value
		memcached.MetaDeleteFlagReturnKey(),          // return CAS value
		memcached.MetaDeleteFlagOpaque(123),          // opaque
		memcached.MetaDeleteFlagRemoveValueOnly(),    // remove value only
	)
	if err != nil {
		panic(err)
	}

	// MetaItem{Key:example:meta:delete Value: CAS:0 Flags:0 TTL:0 LastAccessedTime:0 Size:0 Opaque:123 HitBefore:false}
	fmt.Printf("delete item: %+v\n", item2)

	// get the deleted key, expect key found, but value is nil
	item3, err := client.MetaGet(ctx, key,
		memcached.MetaGetFlagBinaryKey(),
		memcached.MetaGetFlagReturnValue(),
		memcached.MetaGetFlagReturnTTL(),
		memcached.MetaGetFlagReturnCAS(),
		memcached.MetaGetFlagReturnKey(),
		memcached.MetaGetFlagReturnSize(),
		memcached.MetaGetFlagOpaque(123),
		memcached.MetaGetFlagReturnHitBefore(),
		memcached.MetaGetFlagReturnLastAccessedTime(),
		memcached.MetaGetFlagReturnClientFlags(),
	)
	if err != nil {
		panic(err)
	}

	// MetaItem{Key:example:meta:delete Value: CAS:63 Flags:0 TTL:-1 LastAccessedTime:0 Size:0 Opaque:123 HitBefore:false}
	fmt.Printf("get item: %+v\n", item3)
}

func metaArithmetic(client memcached.Client) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	fmt.Println("======== MetaArithmetic example ========")

	key := []byte("example:meta:arithmetic")

	// set a key
	// Increment the key by 100.
	item, err := client.MetaArithmetic(ctx, key, 100,
		memcached.MetaArithmeticFlagInitialValue(200),                            // initial value
		memcached.MetaArithmeticFlagAutoCreate(10),                               // auto create the key if it does not exist
		memcached.MetaArithmeticFlagBinaryKey(),                                  // binary encoded key
		memcached.MetaArithmeticFlagModeSwitch(memcached.MetaArithmeticModeIncr), // increment mode
		memcached.MetaArithmeticFlagOpaque(123),                                  // opaque
		memcached.MetaArithmeticFlagReturnTTL(),                                  // return expiry
		memcached.MetaArithmeticFlagReturnCAS(),                                  // return CAS value
		memcached.MetaArithmeticFlagReturnKey(),                                  // return CAS value
		memcached.MetaArithmeticFlagReturnValue(),                                // return CAS value
	)
	if err != nil {
		panic(err)
	}
	// MetaItem{Key:example:meta:arithmetic Value:10 CAS:56 Flags:0 TTL:100 LastAccessedTime:0 Size:2 Opaque:123 HitBefore:false}
	fmt.Printf("arithmetic item: %+v\n", item)

	// desc the key "foo" by 10 and update
	item2, err := client.MetaArithmetic(ctx, key, 10,
		memcached.MetaArithmeticFlagBinaryKey(),                                  // binary encoded key
		memcached.MetaArithmeticFlagModeSwitch(memcached.MetaArithmeticModeDecr), // decrement mode
		memcached.MetaArithmeticFlagCompareCAS(item.CAS),                         // compare CAS value
		memcached.MetaArithmeticFlagOpaque(123),                                  // opaque
		memcached.MetaArithmeticFlagUpdateTTL(20),                                // update expiry
		memcached.MetaArithmeticFlagReturnTTL(),                                  // return expiry
		memcached.MetaArithmeticFlagReturnCAS(),                                  // return CAS value
		memcached.MetaArithmeticFlagReturnKey(),                                  // return CAS value
		memcached.MetaArithmeticFlagReturnValue(),                                // return CAS value
	)
	if err != nil {
		panic(err)
	}

	// MetaItem{Key:example:meta:arithmetic Value:0 CAS:57 Flags:0 TTL:200 LastAccessedTime:0 Size:1 Opaque:123 HitBefore:false}
	fmt.Printf("arithmetic item2: %+v\n", item2)
}
