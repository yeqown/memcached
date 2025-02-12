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
	item, err := client.MetaSet(ctx, []byte("foo"), []byte("bar"),
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
	// Meta set, key=foo, value=&{Key:[102 111 111] Value:[] CAS:31 Flags:0 TTL:0 LastAccessedTime:0 Size:3 Opaque:456 HitBefore:false}
	fmt.Printf("Meta set, key=%s, value=%+v\n", item.Key, item)

	// Get the value of the key "foo".
	item2, err := client.MetaGet(ctx, []byte("foo"),
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
	// Meta get1, key=foo, value=&{Key:[102 111 111] Value:[98 97 114] CAS:31 Flags:123 TTL:100 LastAccessedTime:0 Size:3 Opaque:789 HitBefore:false}
	fmt.Printf("Meta get1, key=%s, value=%+v\n", item.Key, item2)

	// wait for the key last accessed time to be updated
	time.Sleep(2 * time.Second)

	item3, err := client.MetaGet(ctx, []byte("foo"),
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
	// Meta get2, key=foo, value=&{Key:[102 111 111] Value:[98 97 114] CAS:31 Flags:123 TTL:200 LastAccessedTime:2 Size:3 Opaque:789 HitBefore:true}
	fmt.Printf("Meta get2, key=%s, value=%+v\n", item.Key, item3)
}
