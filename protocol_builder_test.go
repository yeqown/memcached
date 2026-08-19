package memcached

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func Test_protocolBuilder(t *testing.T) {
	// test build a set command
	builder := newProtocolBuilder().
		AddString("set").AddString("key").AddInt(0).AddInt(0).AddInt(5).
		AddCRLF().
		AddString("value")
	expected := "set key 0 0 5\r\nvalue\r\n"
	assert.Equal(t, expected, string(builder.build()))
	builder.release()
}

func Test_protocolBuilder_releaseDiscardsOversizedBuffer(t *testing.T) {
	large := bytes.NewBuffer(make([]byte, 0, maxPooledBufferCap+1024))
	_, _ = large.Write(make([]byte, maxPooledBufferCap+1))
	b := &protocolBuilder{buf: large}
	b.release()
	assert.Nil(t, b.buf)

	// Oversized buffers must not be reused via the pool.
	for i := 0; i < 32; i++ {
		pb := newProtocolBuilder()
		capSize := pb.buf.Cap()
		pb.release()
		assert.LessOrEqual(t, capSize, maxPooledBufferCap)
	}
}

func Test_protocolBuilder_shortCommandsFitDefaultCap(t *testing.T) {
	cases := []struct {
		name  string
		build func() *protocolBuilder
	}{
		{
			name: "get",
			build: func() *protocolBuilder {
				return newProtocolBuilder().AddString("get").AddString("test_key").AddCRLF()
			},
		},
		{
			name: "delete",
			build: func() *protocolBuilder {
				return newProtocolBuilder().AddString("delete").AddString("test_key").AddCRLF()
			},
		},
		{
			name: "set small",
			build: func() *protocolBuilder {
				value := []byte("test_value")
				return newProtocolBuilder().
					AddString("set").AddString("test_key").AddInt(0).AddInt(0).AddInt(len(value)).
					AddCRLF().
					AddBytes(value)
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pb := tc.build()
			defer pb.release()
			assert.LessOrEqual(t, pb.buf.Len(), defaultBufferSize)
			assert.LessOrEqual(t, pb.buf.Cap(), maxPooledBufferCap)
		})
	}
}

func BenchmarkProtocolBuilder_GetShortKey(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		pb := newProtocolBuilder().AddString("get").AddString("test_key").AddCRLF()
		_ = pb.build()
		pb.release()
	}
}

func BenchmarkProtocolBuilder_DeleteShortKey(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		pb := newProtocolBuilder().AddString("delete").AddString("test_key").AddCRLF()
		_ = pb.build()
		pb.release()
	}
}

func BenchmarkProtocolBuilder_SetSmallValue(b *testing.B) {
	b.ReportAllocs()
	value := []byte("test_value")
	for i := 0; i < b.N; i++ {
		pb := newProtocolBuilder().
			AddString("set").AddString("test_key").AddInt(0).AddInt(0).AddInt(len(value)).
			AddCRLF().
			AddBytes(value)
		_ = pb.build()
		pb.release()
	}
}

func BenchmarkProtocolBuilder_MetaSetManyFlags(b *testing.B) {
	b.ReportAllocs()
	value := []byte("test_value")
	for i := 0; i < b.N; i++ {
		pb := newProtocolBuilder().
			AddString("ms").AddBytes([]byte("test_key")).
			AddString("c").AddString("C").AddString("F").AddUint(1).
			AddString("T").AddUint(60).AddString("S").AddInt(len(value)).
			AddCRLF().
			AddBytes(value)
		_ = pb.build()
		pb.release()
	}
}

func BenchmarkProtocolBuilder_SetLargeValue(b *testing.B) {
	b.ReportAllocs()
	value := make([]byte, 4096)
	for i := 0; i < b.N; i++ {
		pb := newProtocolBuilder().
			AddString("set").AddString("test_key").AddInt(0).AddInt(0).AddInt(len(value)).
			AddCRLF().
			AddBytes(value)
		_ = pb.build()
		pb.release()
	}
}

func Test_selectProximateDeadline(t *testing.T) {
	mockNowFunc := func() time.Time {
		// return a fixed time at 2021-07-05 00:00:00 UTC
		return time.Date(2021, 7, 5, 0, 0, 0, 0, time.UTC)
	}
	baseTime := mockNowFunc()

	tests := []struct {
		name         string
		ctx          context.Context
		conn         *mockConn
		timeout      time.Duration
		wantDeadline time.Time
		wantHas      bool
	}{
		{
			name:         "nil context and zero timeout",
			ctx:          nil,
			conn:         newMockConn(),
			timeout:      0,
			wantDeadline: time.Time{},
			wantHas:      false,
		},
		{
			name:         "nil context and negative timeout",
			ctx:          nil,
			conn:         newMockConn(),
			timeout:      -1 * time.Second,
			wantDeadline: time.Time{},
			wantHas:      false,
		},
		{
			name:         "background context and positive timeout",
			ctx:          context.Background(),
			conn:         newMockConn(),
			timeout:      time.Second,
			wantDeadline: baseTime.Add(time.Second),
			wantHas:      true,
		},
		{
			name: "context with deadline and zero timeout",
			ctx: func() context.Context {
				ctx, cancel := context.WithDeadline(context.Background(), baseTime.Add(2*time.Second))
				t.Cleanup(cancel)
				return ctx
			}(),
			conn:         newMockConn(),
			timeout:      0,
			wantDeadline: baseTime.Add(2 * time.Second),
			wantHas:      true,
		},
		{
			name: "context with earlier deadline",
			ctx: func() context.Context {
				ctx, cancel := context.WithDeadline(context.Background(), baseTime.Add(time.Second))
				t.Cleanup(cancel)
				return ctx
			}(),
			conn:         newMockConn(),
			timeout:      2 * time.Second,
			wantDeadline: baseTime.Add(time.Second),
			wantHas:      true,
		},
		{
			name: "timeout earlier than context deadline",
			ctx: func() context.Context {
				ctx, cancel := context.WithDeadline(context.Background(), baseTime.Add(2*time.Second))
				t.Cleanup(cancel)
				return ctx
			}(),
			conn:         newMockConn(),
			timeout:      time.Second,
			wantDeadline: baseTime.Add(time.Second),
			wantHas:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotHas := selectProximateDeadline(tt.ctx, tt.conn, tt.timeout, mockNowFunc, true)
			assert.Equal(t, tt.wantHas, gotHas)
			assert.Equal(t, tt.wantDeadline, tt.conn.readDeadline)
		})
	}
}
