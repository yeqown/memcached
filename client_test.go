package memcached

import (
	"context"
	"sync"
	"testing"
	"time"

	pkgerrors "github.com/pkg/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

type clientTestSuite struct {
	suite.Suite

	client *client
}

func (su *clientTestSuite) SetupSuite() {
	addrs := "localhost:11211"
	c, err := newClientWithContext(context.Background(), addrs)
	su.Require().NoError(err)
	su.client = c.(*client)
}

func (su *clientTestSuite) TearDownSuite() {
	err := su.client.Close()
	su.Require().NoError(err)
}

func (su *clientTestSuite) newCompressedClient() *client {
	c, err := newClientWithContext(
		context.Background(),
		"localhost:11211",
		WithDefaultCompression(CompressionAlgorithmDeflate),
		WithCompressionThreshold(1),
	)
	require.NoError(su.T(), err)

	cc := c.(*client)
	su.T().Cleanup(func() {
		require.NoError(su.T(), cc.Close())
	})
	return cc
}

func (su *clientTestSuite) Test_concurrent_dispatchRequest() {
	key := "Test_concurrent_dispatchRequest"
	// prepare data

	ctx := context.Background()

	err := su.client.Set(ctx, key, []byte("Test_concurrent_dispatchRequest"), 0, 0)
	su.Require().NoError(err)

	wg := sync.WaitGroup{}
	limits := 100
	for i := 0; i < 10; i++ {
		wg.Add(1)

		go func() {
			defer wg.Done()
			for counter := 0; counter < limits; counter++ {
				req, resp := buildGetsCommand("get", key)
				err := su.client.dispatchRequest(ctx, req, resp)
				su.Require().NoError(err)
			}
		}()
	}

	wg.Wait()
}

// https://github.com/yeqown/memcached/issues/18
// Mock a concurrent client to set, get and touch the cache at the same time.
func (su *clientTestSuite) Test_concurrent() {
	// metaset options
	msOptions := func(
		cas uint64, clientFlags uint16,
	) []MetaSetOption {
		expiration := 2
		return []MetaSetOption{
			MetaSetFlagNewCAS(cas),
			MetaSetFlagTTL(uint64(expiration)),
			MetaSetFlagClientFlags(clientFlags),
			MetaSetFlagReturnKey(),
			MetaSetFlagReturnSize(),
			MetaSetFlagReturnCAS(),
		}
	}

	key := "Test_concurrent"
	value := "Test_concurrent is value of Test_concurrent"
	// prepare data
	ctx := context.Background()

	wg := sync.WaitGroup{}
	wg.Add(3)

	// meta set goroutine
	go func() {
		defer wg.Done()

		counter := 0
		cas := uint64(0)

		// update only
		for counter <= 20 {
			item, err := su.client.MetaSet(ctx, []byte(key), []byte(value), msOptions(cas, 0x1234)...)
			su.NoError(err)

			cas = item.CAS
			counter++

			time.Sleep(time.Second * 5)
		}
	}()

	// get goroutine
	go func() {
		defer wg.Done()

		counter := 0

		for counter <= 1000 {
			item, err := su.client.Get(ctx, key)
			if pkgerrors.Is(err, ErrNotFound) {
				goto next
			}
			su.NoError(err)

			su.Equal(value, string(item.Value))
			counter++

		next:
			time.Sleep(time.Millisecond * 100)
		}
	}()

	// touch goroutine
	go func() {
		defer wg.Done()
		counter := 0

		for counter <= 100 {
			err := su.client.Touch(ctx, key, 3)
			if !pkgerrors.Is(err, ErrNotFound) {
				su.NoError(err)
			}

			counter++

			time.Sleep(time.Second)
		}
	}()

	wg.Wait()

	su.T().Log("Test_concurrent finished")
}

func (su *clientTestSuite) Test_compressionClassicReadCommandsRoundTrip() {
	ctx := context.Background()
	client := su.newCompressedClient()

	value := []byte("hello hello hello hello hello hello")
	appFlags := uint16(0x1234)
	key1 := "Test_compressionClassicReadCommandsRoundTrip_1"
	key2 := "Test_compressionClassicReadCommandsRoundTrip_2"

	su.Require().NoError(client.Set(ctx, key1, value, appFlags, 0))
	su.Require().NoError(client.Set(ctx, key2, value, appFlags, 0))

	assertItem := func(item *Item) {
		su.Require().NotNil(item)
		su.Equal(value, item.Value)
		su.False(item.Flags.unconventional())
		su.True(item.Flags.isCompressed())
		su.Equal(uint32(appFlags), item.Flags.AppFlags())
	}

	item, err := client.Get(ctx, key1)
	su.Require().NoError(err)
	assertItem(item)

	items, err := client.Gets(ctx, key1, key2)
	su.Require().NoError(err)
	su.Require().Len(items, 2)
	for _, item := range items {
		assertItem(item)
		su.NotZero(item.CAS)
	}

	item, err = client.GetAndTouch(ctx, time.Second, key1)
	su.Require().NoError(err)
	assertItem(item)

	items, err = client.GetAndTouches(ctx, time.Second, key1, key2)
	su.Require().NoError(err)
	su.Require().Len(items, 2)
	for _, item := range items {
		assertItem(item)
		su.NotZero(item.CAS)
	}
}

func (su *clientTestSuite) Test_compressionMetaReadTransparency() {
	ctx := context.Background()
	client := su.newCompressedClient()

	key := []byte("Test_compressionMetaReadTransparency")
	value := []byte("hello hello hello hello hello hello")
	appFlags := uint16(0x2345)

	stored, err := client.MetaSet(ctx, key, value, MetaSetFlagClientFlags(appFlags))
	su.Require().NoError(err)
	su.False(stored.Flags.unconventional())
	su.True(stored.Flags.isCompressed())
	su.Equal(uint32(appFlags), stored.Flags.AppFlags())

	item, err := client.MetaGet(ctx, key, MetaGetFlagReturnValue())
	su.Require().NoError(err)
	su.Equal(value, item.Value)
	su.Zero(item.Flags)

	item, err = client.MetaGet(ctx, key, MetaGetFlagReturnValue(), MetaGetFlagReturnClientFlags())
	su.Require().NoError(err)
	su.Equal(value, item.Value)
	su.False(item.Flags.unconventional())
	su.True(item.Flags.isCompressed())
	su.Equal(uint32(appFlags), item.Flags.AppFlags())
}

func TestCompressionDisablesAppendPrepend(t *testing.T) {
	client := &client{
		options: &clientOptions{
			compressAlg: CompressionAlgorithmDeflate,
		},
	}

	err := client.Append(context.Background(), "key", []byte("value"), 0, 0)
	require.Error(t, err)
	assert.True(t, pkgerrors.Is(err, ErrNotSupported))

	err = client.Prepend(context.Background(), "key", []byte("value"), 0, 0)
	require.Error(t, err)
	assert.True(t, pkgerrors.Is(err, ErrNotSupported))
}

func TestCompressionDisablesMetaAppendPrepend(t *testing.T) {
	client := &client{
		options: &clientOptions{
			compressAlg: CompressionAlgorithmDeflate,
		},
	}

	errModes := []metaSetMode{MetaSetModeAppend, MetaSetModePrepend}
	for _, mode := range errModes {
		t.Run(string(mode), func(t *testing.T) {
			item, err := client.MetaSet(
				context.Background(),
				[]byte("key"),
				[]byte("value"),
				MetaSetFlagModeSwitch(mode),
			)
			assert.Nil(t, item)
			require.Error(t, err)
			assert.True(t, pkgerrors.Is(err, ErrNotSupported))
		})
	}
}

func TestClientSuite(t *testing.T) {
	suite.Run(t, new(clientTestSuite))
}

func Test_udp(t *testing.T) {
	t.Skipf("skip test_udp, since it could not run in CI")

	addrs := "udp://localhost:11211"
	c, err := newClientWithContext(context.Background(), addrs, WithUDPEnabled())
	if err != nil {
		t.Fatalf("Failed to create client: %+v", err)
	}
	assert.NoError(t, err)
	assert.NotNil(t, c)

	ver, err := c.Version(context.Background())
	if err != nil {
		t.Fatalf("Failed to get version: %+v", err)
	}
	assert.NoError(t, err)
	assert.NotEmpty(t, ver)

	t.Logf("version: %s", ver)
}

func Test_unix(t *testing.T) {
	t.Skipf("skip test_unix, since it could not run in CI")

	addrs := "unix:///tmp/memcached.sock"
	c, err := newClientWithContext(context.Background(), addrs)
	require.NoError(t, err)
	require.NotNil(t, c)

	ver, err := c.Version(context.Background())
	require.NoError(t, err)
	require.NotEmpty(t, ver)

	t.Logf("version: %s", ver)
}
