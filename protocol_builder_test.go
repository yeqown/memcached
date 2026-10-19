package memcached

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_ProtocolBuilder(t *testing.T) {
	builder := newProtocolBuilder().
		AddString("set").AddString("key").AddInt(0).AddInt(0).AddInt(5).
		AddCRLF().
		AddString("value")
	defer builder.release()

	assert.Equal(t, "set key 0 0 5\r\nvalue\r\n", string(builder.build()))
}

func Test_ProtocolDeadlineSelection(t *testing.T) {
	baseTime := time.Date(2021, 7, 5, 0, 0, 0, 0, time.UTC)
	now := func() time.Time { return baseTime }
	for _, tt := range []struct {
		name         string
		timeout      time.Duration
		ctxDeadline  time.Time
		wantDeadline time.Time
	}{
		{name: "nil context and zero timeout"},
		{name: "nil context and negative timeout", timeout: -time.Second},
		{name: "nil context and positive timeout", timeout: time.Second, wantDeadline: baseTime.Add(time.Second)},
		{
			name:        "context deadline alone",
			ctxDeadline: baseTime.Add(2 * time.Second), wantDeadline: baseTime.Add(2 * time.Second),
		},
		{
			name: "context deadline earlier", timeout: 2 * time.Second,
			ctxDeadline: baseTime.Add(time.Second), wantDeadline: baseTime.Add(time.Second),
		},
		{
			name: "timeout earlier", timeout: time.Second,
			ctxDeadline: baseTime.Add(2 * time.Second), wantDeadline: baseTime.Add(time.Second),
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var ctx context.Context
			if !tt.ctxDeadline.IsZero() {
				var cancel context.CancelFunc
				ctx, cancel = context.WithDeadline(context.Background(), tt.ctxDeadline)
				t.Cleanup(cancel)
			}
			for _, direction := range []string{"read", "write"} {
				t.Run(direction, func(t *testing.T) {
					cn := newTestConn()
					isRead := direction == "read"
					has := selectProximateDeadline(ctx, cn, tt.timeout, now, isRead)
					assert.Equal(t, !tt.wantDeadline.IsZero(), has)
					if isRead {
						assert.Equal(t, tt.wantDeadline, cn.readDeadline)
						assert.Zero(t, cn.writeDeadline)
					} else {
						assert.Equal(t, tt.wantDeadline, cn.writeDeadline)
						assert.Zero(t, cn.readDeadline)
					}
				})
			}
		})
	}
}

func udpDatagram(payload []byte) []byte {
	return append([]byte{0, 1, 0, 0, 0, 1, 0, 0}, payload...)
}

func Test_UDPSend(t *testing.T) {
	req, resp := buildGetsCommand("get", "key")
	defer releaseReqAndResp(req, resp)
	req.udpEnabled = true
	cn := newTestConn()

	require.NoError(t, req.send(context.Background(), cn, 0))
	require.Len(t, cn.writes, 1)
	assert.Equal(t, []byte("\x00\x01\x00\x00\x00\x01\x00\x00get key\r\n"), cn.writes[0])
	assert.Equal(t, []byte("get key\r\n"), req.raw)

	cn.writeErr = io.ErrClosedPipe
	assert.ErrorIs(t, req.sendUDP(cn), io.ErrClosedPipe)
}

func Test_parseUDPHeader(t *testing.T) {
	for _, tt := range []struct {
		name string
		raw  []byte
		want []byte
	}{
		{name: "empty"},
		{name: "short header", raw: []byte{0, 1, 0, 0, 0, 1, 0}, want: []byte{0, 1, 0, 0, 0, 1, 0}},
		{name: "header alone", raw: udpDatagram(nil), want: []byte{}},
		{name: "payload", raw: udpDatagram([]byte("END\r\n")), want: []byte("END\r\n")},
	} {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, parseUDPHeader(tt.raw))
		})
	}
}

func Test_UDPReceive(t *testing.T) {
	for _, tt := range []struct {
		name     string
		build    func() *response
		received [][]byte
		want     [][]byte
		wantErr  error
	}{
		{
			name:  "limited lines strip header only once",
			build: func() *response { return buildLimitedLineResponse(2) },
			received: [][]byte{
				udpDatagram([]byte("VA 9\r\n")), []byte("123456789\r\n"),
			},
			want: [][]byte{[]byte("VA 9\r\n"), []byte("123456789\r\n")},
		},
		{
			name:  "specific end line strips header only once",
			build: func() *response { return buildSpecEndLineResponse([]byte("END\r\n"), 3) },
			received: [][]byte{
				udpDatagram([]byte("VALUE key 0 9\r\n")), []byte("123456789\r\n"), []byte("END\r\n"),
			},
			want: [][]byte{[]byte("VALUE key 0 9\r\n"), []byte("123456789\r\n"), []byte("END\r\n")},
		},
		{
			name:     "empty result includes end line",
			build:    func() *response { return buildSpecEndLineResponse([]byte("END\r\n"), 1) },
			received: [][]byte{udpDatagram([]byte("END\r\n"))},
			want:     [][]byte{[]byte("END\r\n")},
		},
		{
			name:     "limited response detects error after header",
			build:    func() *response { return buildLimitedLineResponse(2) },
			received: [][]byte{udpDatagram([]byte("EN kfoo\r\n"))},
			wantErr:  ErrNotFound,
		},
		{
			name:     "specific end response detects error after header",
			build:    func() *response { return buildSpecEndLineResponse([]byte("END\r\n"), 1) },
			received: [][]byte{udpDatagram([]byte("SERVER_ERROR unavailable\r\n"))},
			wantErr:  ErrServerError,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			resp := tt.build()
			defer resp.release()
			resp.udpEnabled = true

			err := resp.recv(context.Background(), newTestConn(tt.received...), 0)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				assert.Empty(t, resp.rawLines)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, resp.rawLines)
		})
	}
}

func Test_ProtocolFaultLines(t *testing.T) {
	for _, tt := range []struct {
		line    string
		wantErr error
	}{
		{line: "STORED\r\n"},
		{line: "HD c42\r\n"},
		{line: "ERROR\r\n", wantErr: ErrNonexistentCommand},
		{line: "CLIENT_ERROR invalid key\r\n", wantErr: ErrClientError},
		{line: "SERVER_ERROR unavailable\r\n", wantErr: ErrServerError},
		{line: "NOT_FOUND\r\n", wantErr: ErrNotFound},
		{line: "NOT_STORED\r\n", wantErr: ErrNotStored},
		{line: "EXISTS\r\n", wantErr: ErrExists},
		{line: "NF\r\n", wantErr: ErrNotFound},
		{line: "EN kfoo\r\n", wantErr: ErrNotFound},
		{line: "NS O42\r\n", wantErr: ErrNotStored},
		{line: "EX c42\r\n", wantErr: ErrExists},
	} {
		t.Run(strings.TrimSpace(tt.line), func(t *testing.T) {
			err := forecastCommonFaultLine([]byte(tt.line))
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
		})
	}
}

func Test_ProtocolResponseStatus(t *testing.T) {
	t.Run("matching status", func(t *testing.T) {
		resp := buildLimitedLineResponse(1)
		defer resp.release()
		require.NoError(t, resp.recv(context.Background(), newTestConn([]byte("STORED\r\n")), 0))
		assert.NoError(t, resp.expect([]byte("STORED\r\n")))
	})

	t.Run("unexpected status", func(t *testing.T) {
		resp := buildLimitedLineResponse(1)
		defer resp.release()
		require.NoError(t, resp.recv(context.Background(), newTestConn([]byte("DELETED\r\n")), 0))
		assert.ErrorContains(t, resp.expect([]byte("STORED\r\n")), "unexpected response")
	})

	t.Run("status requires one line", func(t *testing.T) {
		resp := buildLimitedLineResponse(2)
		defer resp.release()
		require.NoError(t, resp.recv(context.Background(), newTestConn([]byte("STORED\r\nSTORED\r\n")), 0))
		assert.ErrorIs(t, resp.expect([]byte("STORED\r\n")), ErrMalformedResponse)
	})

	t.Run("quiet response does not read", func(t *testing.T) {
		resp := buildNoReplyResponse()
		defer resp.release()
		cn := newTestConn()
		cn.readErr = io.ErrUnexpectedEOF
		require.NoError(t, resp.recv(context.Background(), cn, 0))
		assert.NoError(t, resp.expect([]byte("STORED\r\n")))
	})

	t.Run("read failure", func(t *testing.T) {
		resp := buildLimitedLineResponse(1)
		defer resp.release()
		cn := newTestConn()
		cn.readErr = io.ErrUnexpectedEOF
		assert.ErrorIs(t, resp.recv(context.Background(), cn, 0), io.ErrUnexpectedEOF)
	})

	t.Run("unknown response framing", func(t *testing.T) {
		resp := &response{}
		assert.ErrorIs(t, resp.recv(context.Background(), newTestConn(), 0), ErrUnknownIndicator)
	})
}
