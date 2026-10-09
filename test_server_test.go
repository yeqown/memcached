package memcached

import (
	"net"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

// startTCPTestServer stops accepting and closes active connections before
// waiting for handlers, so cleanup also interrupts blocked protocol reads.
func startTCPTestServer(t *testing.T, serve func(net.Conn)) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	var connections sync.Map
	var handlers sync.WaitGroup
	acceptDone := make(chan struct{})
	go func() {
		defer close(acceptDone)
		for {
			cn, err := listener.Accept()
			if err != nil {
				return
			}
			connections.Store(cn, struct{}{})
			handlers.Go(func() {
				defer connections.Delete(cn)
				defer func() { _ = cn.Close() }()
				serve(cn)
			})
		}
	}()
	t.Cleanup(func() {
		require.NoError(t, listener.Close())
		<-acceptDone
		connections.Range(func(key, _ any) bool {
			_ = key.(net.Conn).Close()
			return true
		})
		handlers.Wait()
	})
	return listener.Addr().String()
}
