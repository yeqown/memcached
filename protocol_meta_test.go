package memcached

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	memcodec "github.com/yeqown/memcached/codec"
)

func Test_parseFlags(t *testing.T) {
	for _, tt := range []struct {
		name     string
		line     string
		startPos int
		want     MetaItem
	}{
		{
			name: "header flags", line: "HD c26 kZm9v b O456 s3", startPos: 1,
			want: MetaItem{CAS: 26, Size: 3, Opaque: 456},
		},
		{
			name: "value flags", line: "VA 3 c29 f123 h1 kZm9v b l7 O789 s3 t200", startPos: 2,
			want: MetaItem{CAS: 29, Flags: 123, TTL: 200, LastAccessedTime: 7, Size: 3, Opaque: 789, HitBefore: true},
		},
		{
			name: "nonexpiring TTL and miss", line: "HD t-1 h0", startPos: 1,
			want: MetaItem{TTL: -1},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			item := &MetaItem{}
			parseFlags(bytes.Fields([]byte(tt.line)), tt.startPos, item)
			assert.Equal(t, tt.want, *item)
		})
	}
}

func Test_parseMetaItem(t *testing.T) {
	for _, tt := range []struct {
		name    string
		lines   [][]byte
		noReply bool
		want    MetaItem
		wantErr error
	}{
		{name: "not found", lines: [][]byte{[]byte("NF\r\n")}, wantErr: ErrNotFound},
		{name: "quiet miss", lines: [][]byte{[]byte("EN\r\n")}, noReply: true, wantErr: ErrNotFound},
		{name: "miss with flags", lines: [][]byte{[]byte("EN kfoo\r\n")}, wantErr: ErrNotFound},
		{name: "not stored", lines: [][]byte{[]byte("NS\r\n")}, wantErr: ErrNotStored},
		{name: "CAS conflict", lines: [][]byte{[]byte("EX\r\n")}, wantErr: ErrExists},
		{name: "missing response", wantErr: ErrMalformedResponse},
		{name: "quiet without response", noReply: true},
		{
			name:  "header",
			lines: [][]byte{[]byte("HD c26 kZm9v b O456 s3\r\n")},
			want:  MetaItem{CAS: 26, Size: 3, Opaque: 456},
		},
		{
			name: "value",
			lines: [][]byte{
				[]byte("VA 3 c29 f123 h1 kZm9v b l0 O789 s3 t200\r\n"), []byte("bar\r\n"),
			},
			want: MetaItem{Value: []byte("bar"), CAS: 29, Flags: 123, TTL: 200, Size: 3, Opaque: 789, HitBefore: true},
		},
		{
			name:    "missing value",
			lines:   [][]byte{[]byte("VA 3 c29 f123 h1 kZm9v b l0 O789 s3 t200\r\n")},
			wantErr: ErrMalformedResponse,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			item := &MetaItem{}
			err := parseMetaItem(tt.lines, item, tt.noReply, memcodec.Noop)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, *item)
		})
	}
}

func Test_parseMetaItemCodec(t *testing.T) {
	src := []byte("hello hello hello hello hello hello")
	codec := mustCompressCodec(t, memcodec.CompressionAlgorithmDeflate, 1, 6)
	compressed, flags, err := codec.Encode([]byte("foo"), src, 0x12)
	require.NoError(t, err)
	lines := [][]byte{
		[]byte(fmt.Sprintf("VA %d f%d c29 O789\r\n", len(compressed), flags)),
		append(append([]byte(nil), compressed...), '\r', '\n'),
	}

	for _, tt := range []struct {
		name  string
		codec Codec
		value []byte
		flags uint32
	}{
		{name: "noop preserves wire value and flags", codec: memcodec.Noop, value: compressed, flags: flags},
		{name: "compression decodes value and application flags", codec: codec, value: src, flags: 0x12},
	} {
		t.Run(tt.name, func(t *testing.T) {
			item := &MetaItem{Key: []byte("foo")}
			require.NoError(t, parseMetaItem(lines, item, false, tt.codec))
			assert.Equal(t, &MetaItem{
				Key: []byte("foo"), Value: tt.value, Flags: tt.flags,
				Size: uint64(len(compressed)), CAS: 29, Opaque: 789,
			}, item)
		})
	}
}

func Test_buildMetaArithmeticCommand(t *testing.T) {
	for _, tt := range []struct {
		name      string
		flags     metaArithmeticFlags
		wantRaw   string
		indicator responseEndIndicator
		lines     uint8
	}{
		{
			name: "binary quiet decrement",
			flags: metaArithmeticFlags{
				b: true, C: 1, E: 2, N: 3, J: 4, D: 5, T: 6, M: MetaArithmeticModeDecr,
				O: 7, q: true, t: true, c: true, v: true, k: true,
			},
			wantRaw:   "ma Zm9v b C1 E2 N3 J4 D64 T6 MD O7 q t c v k\r\n",
			indicator: endIndicatorNoReply,
		},
		{
			name: "increment with value",
			flags: metaArithmeticFlags{
				C: 1, E: 2, N: 3, J: 4, D: 5, T: 6, M: MetaArithmeticModeIncr,
				O: 7, t: true, c: true, v: true, k: true,
			},
			wantRaw:   "ma foo C1 E2 N3 J4 D64 T6 MI O7 t c v k\r\n",
			indicator: endIndicatorLimitedLines, lines: 2,
		},
		{
			name: "increment without value", wantRaw: "ma foo D64\r\n",
			indicator: endIndicatorLimitedLines, lines: 1,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			req, resp := buildMetaArithmeticCommand([]byte("foo"), 64, &tt.flags)
			defer releaseReqAndResp(req, resp)

			assert.Equal(t, tt.wantRaw, string(req.raw))
			assert.Equal(t, tt.indicator, resp.endIndicator)
			assert.Equal(t, tt.lines, resp.limitedLines)
		})
	}
}

func Test_buildMetaGetCommand(t *testing.T) {
	for _, tt := range []struct {
		name      string
		flags     metaGetFlags
		wantRaw   string
		wantKey   string
		indicator responseEndIndicator
		lines     uint8
	}{
		{
			name: "binary quiet with all flags",
			flags: metaGetFlags{
				b: true, c: true, f: true, h: true, k: true, l: true, O: 1, q: true,
				s: true, t: true, u: true, v: true, E: 2, N: 3, R: 4, T: 5,
			},
			wantRaw: "mg Zm9v b c f h k l O1 q s t u v E2 N3 R4 T5\r\n", wantKey: "Zm9v",
			indicator: endIndicatorNoReply,
		},
		{
			name: "plain key with all flags",
			flags: metaGetFlags{
				c: true, f: true, h: true, k: true, l: true, O: 1,
				s: true, t: true, u: true, v: true, E: 2, N: 3, R: 4, T: 5,
			},
			wantRaw: "mg foo c f h k l O1 s t u v E2 N3 R4 T5\r\n", wantKey: "foo",
			indicator: endIndicatorLimitedLines, lines: 2,
		},
		{
			name: "value and client flags", flags: metaGetFlags{v: true, f: true},
			wantRaw: "mg foo f v\r\n", wantKey: "foo",
			indicator: endIndicatorLimitedLines, lines: 2,
		},
		{
			name: "metadata only", wantRaw: "mg foo\r\n", wantKey: "foo",
			indicator: endIndicatorLimitedLines, lines: 1,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			req, resp := buildMetaGetCommand([]byte("foo"), &tt.flags)
			defer releaseReqAndResp(req, resp)

			assert.Equal(t, tt.wantRaw, string(req.raw))
			assert.Equal(t, []byte(tt.wantKey), req.key)
			assert.Equal(t, tt.indicator, resp.endIndicator)
			assert.Equal(t, tt.lines, resp.limitedLines)
		})
	}
}

func Test_buildMetaSetCommand(t *testing.T) {
	for _, tt := range []struct {
		name      string
		flags     metaSetFlags
		wantRaw   string
		indicator responseEndIndicator
	}{
		{
			name: "binary quiet set",
			flags: metaSetFlags{
				b: true, c: true, C: 1, E: 2, F: 3, I: true, k: true, O: 4,
				q: true, s: true, T: 5, M: MetaSetModeSet, N: 6,
			},
			wantRaw:   "ms Zm9v 3 b c C1 E2 F3 I k O4 q s T5 Mset N6\r\nbar\r\n",
			indicator: endIndicatorNoReply,
		},
		{
			name: "replace",
			flags: metaSetFlags{
				c: true, C: 1, E: 2, F: 3, I: true, k: true, O: 4,
				s: true, T: 5, M: MetaSetModeReplace, N: 6,
			},
			wantRaw:   "ms foo 3 c C1 E2 F3 I k O4 s T5 Mreplace N6\r\nbar\r\n",
			indicator: endIndicatorLimitedLines,
		},
		{
			name: "omits F0", wantRaw: "ms foo 3\r\nbar\r\n",
			indicator: endIndicatorLimitedLines,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			req, resp, err := buildMetaSetCommand([]byte("foo"), []byte("bar"), &tt.flags, memcodec.Noop)
			defer releaseReqAndResp(req, resp)
			require.NoError(t, err)

			assert.Equal(t, tt.wantRaw, string(req.raw))
			assert.Equal(t, tt.indicator, resp.endIndicator)
		})
	}
}

func Test_buildMetaDeleteCommand(t *testing.T) {
	for _, tt := range []struct {
		name      string
		flags     metaDeleteFlags
		wantRaw   string
		indicator responseEndIndicator
	}{
		{
			name:    "binary quiet delete",
			flags:   metaDeleteFlags{b: true, C: 1, E: 2, I: true, k: true, O: 3, q: true, T: 4, x: true},
			wantRaw: "md Zm9v b C1 E2 I k O3 q T4 x\r\n", indicator: endIndicatorNoReply,
		},
		{
			name:    "plain key with flags",
			flags:   metaDeleteFlags{C: 1, E: 2, I: true, k: true, O: 3, T: 4, x: true},
			wantRaw: "md foo C1 E2 I k O3 T4 x\r\n", indicator: endIndicatorLimitedLines,
		},
		{
			name: "default", wantRaw: "md foo\r\n", indicator: endIndicatorLimitedLines,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			req, resp := buildMetaDeleteCommand([]byte("foo"), &tt.flags)
			defer releaseReqAndResp(req, resp)

			assert.Equal(t, tt.wantRaw, string(req.raw))
			assert.Equal(t, tt.indicator, resp.endIndicator)
		})
	}
}

func Test_buildMetaDebugCommand(t *testing.T) {
	for _, tt := range []struct {
		name    string
		flags   metaDebugFlags
		wantRaw string
	}{
		{name: "plain key", wantRaw: "me foo\r\n"},
		{name: "binary key", flags: metaDebugFlags{b: true}, wantRaw: "me Zm9v b\r\n"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			req, resp := buildMetaDebugCommand([]byte("foo"), &tt.flags)
			defer releaseReqAndResp(req, resp)

			assert.Equal(t, tt.wantRaw, string(req.raw))
			assert.Equal(t, endIndicatorLimitedLines, resp.endIndicator)
			assert.Equal(t, uint8(1), resp.limitedLines)
		})
	}
}

func Test_parseMetaItemDebug(t *testing.T) {
	for _, tt := range []struct {
		name    string
		lines   [][]byte
		want    MetaItemDebug
		wantErr error
	}{
		{
			name:  "metadata",
			lines: [][]byte{[]byte("ME foo exp=-1 la=2 cas=18 fetch=yes cls=1 size=65\r\n")},
			want:  MetaItemDebug{TTL: -1, LastAssessTime: 2, CAS: 18, HitBefore: true, SlabClassID: 1, Size: 65},
		},
		{name: "not found", lines: [][]byte{[]byte("EN\r\n")}, wantErr: ErrNotFound},
		{name: "missing response", wantErr: ErrMalformedResponse},
		{name: "unexpected status", lines: [][]byte{[]byte("HD\r\n")}, wantErr: ErrMalformedResponse},
	} {
		t.Run(tt.name, func(t *testing.T) {
			item := &MetaItemDebug{}
			err := parseMetaItemDebug(tt.lines, item)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, *item)
		})
	}
}
