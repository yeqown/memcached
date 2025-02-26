package memcached

import (
	"context"
	"sync"
	"testing"

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

func TestClientSuite(t *testing.T) {
	suite.Run(t, new(clientTestSuite))
}
