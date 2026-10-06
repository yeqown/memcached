package memcached

import (
	"context"
	"io"
	"net"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/yeqown/memcached/resolver"
)

func TestNewConnContext(t *testing.T) {
	for _, network := range []string{"tcp", "unix"} {
		t.Run(network, func(t *testing.T) {
			address := "127.0.0.1:0"
			if network == "unix" {
				address = filepath.Join(t.TempDir(), "memcached.sock")
			}
			listener, err := net.Listen(network, address)
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, listener.Close()) })
			done := make(chan error, 1)
			go func() {
				raw, err := listener.Accept()
				if err != nil {
					done <- err
					return
				}
				defer func() { _ = raw.Close() }()
				if err := raw.SetDeadline(time.Now().Add(time.Second)); err != nil {
					done <- err
					return
				}
				request := make([]byte, len("version\r\n"))
				if _, err := io.ReadFull(raw, request); err != nil {
					done <- err
					return
				}
				if string(request) != "version\r\n" {
					done <- io.ErrUnexpectedEOF
					return
				}
				_, err = io.WriteString(raw, "VERSION test\r\n")
				done <- err
			}()
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			cn, err := newConnContext(ctx, resolver.NewAddr(network, listener.Addr().String(), 0), time.Second)
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, cn.Close()) })
			require.NoError(t, cn.setReadDeadline(time.Now().Add(time.Second)))
			require.NoError(t, cn.setWriteDeadline(time.Now().Add(time.Second)))
			_, err = cn.Write([]byte("version\r\n"))
			require.NoError(t, err)
			line, err := cn.readLine('\n')
			require.NoError(t, err)
			require.Equal(t, "VERSION test\r\n", string(line))
			require.NoError(t, <-done)
			require.NoError(t, cn.Close())
			require.NoError(t, cn.Close(), "closing a connection is idempotent")
			_, err = cn.Read(make([]byte, 1))
			require.Error(t, err)
			_, err = cn.Write([]byte("version\r\n"))
			require.Error(t, err)
			_, err = cn.readLine('\n')
			require.Error(t, err)
		})
	}
}

func TestNewConnContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	cn, err := newConnContext(ctx, resolver.NewAddr("tcp", "127.0.0.1:11211", 0), time.Second)
	require.ErrorIs(t, err, context.Canceled)
	require.Nil(t, cn)
}

func TestNewConnContextUDP(t *testing.T) {
	listener, err := net.ListenPacket("udp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, listener.Close()) })
	require.NoError(t, listener.SetDeadline(time.Now().Add(time.Second)))
	done := make(chan error, 1)
	go func() {
		buffer := make([]byte, 64)
		n, address, err := listener.ReadFrom(buffer)
		if err != nil {
			done <- err
			return
		}
		if string(buffer[:n]) != "version\r\n" {
			done <- io.ErrUnexpectedEOF
			return
		}
		_, err = listener.WriteTo([]byte("VERSION test\r\n"), address)
		done <- err
	}()
	cn, err := newConnContext(t.Context(), resolver.NewAddr("udp", listener.LocalAddr().String(), 0), time.Second)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, cn.Close()) })
	require.NoError(t, cn.setReadDeadline(time.Now().Add(time.Second)))
	_, err = cn.Write([]byte("version\r\n"))
	require.NoError(t, err)
	line, err := cn.readLine('\n')
	require.NoError(t, err)
	require.Equal(t, "VERSION test\r\n", string(line))
	require.NoError(t, <-done)
}
