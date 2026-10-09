package memcached

import (
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	memcodec "github.com/yeqown/memcached/codec"
)

func Test_parseValueItems(t *testing.T) {
	tests := []struct {
		name           string
		lines          [][]byte
		withoutEndLine bool
		withCAS        bool
		want           []*Item
		wantErr        error
	}{
		{
			name: "multiple values",
			lines: [][]byte{
				[]byte("VALUE key 0 5\r\n"), []byte("value\r\n"),
				[]byte("VALUE key2 0 6\r\n"), []byte("value2\r\n"),
				[]byte("END\r\n"),
			},
			want: []*Item{
				{Key: "key", Value: []byte("value")},
				{Key: "key2", Value: []byte("value2")},
			},
		},
		{
			name: "flags and CAS",
			lines: [][]byte{
				[]byte("VALUE key 123 5 1\r\n"), []byte("value\r\n"),
				[]byte("VALUE key2 123 6 2\r\n"), []byte("value2\r\n"),
				[]byte("END\r\n"),
			},
			withCAS: true,
			want: []*Item{
				{Key: "key", Value: []byte("value"), Flags: 123, CAS: 1},
				{Key: "key2", Value: []byte("value2"), Flags: 123, CAS: 2},
			},
		},
		{
			name: "without end line",
			lines: [][]byte{
				[]byte("VALUE key 123 5 1\r\n"), []byte("value\r\n"),
				[]byte("VALUE key2 123 6 2\r\n"), []byte("value2\r\n"),
			},
			withoutEndLine: true,
			withCAS:        true,
			want: []*Item{
				{Key: "key", Value: []byte("value"), Flags: 123, CAS: 1},
				{Key: "key2", Value: []byte("value2"), Flags: 123, CAS: 2},
			},
		},
		{
			name:  "no values",
			lines: [][]byte{[]byte("END\r\n")},
			want:  []*Item{},
		},
		{
			name: "negative flags",
			lines: [][]byte{
				[]byte("VALUE key -1 5\r\n"), []byte("value\r\n"), []byte("END\r\n"),
			},
			wantErr: ErrMalformedResponse,
		},
		{
			name:           "missing data block",
			lines:          [][]byte{[]byte("VALUE key 0 5\r\n")},
			withoutEndLine: true,
			wantErr:        ErrMalformedResponse,
		},
		{
			name: "data length mismatch",
			lines: [][]byte{
				[]byte("VALUE key 0 4\r\n"), []byte("value\r\n"), []byte("END\r\n"),
			},
			wantErr: ErrMalformedResponse,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseValueItems(tt.lines, tt.withoutEndLine, tt.withCAS, memcodec.Noop)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				assert.Nil(t, got)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func Test_parseValueItemsCodec(t *testing.T) {
	src := []byte("hello hello hello hello hello hello")
	codec := mustCompressCodec(t, memcodec.CompressionAlgorithmDeflate, 1, 6)
	compressed, flags, err := codec.Encode([]byte("key"), src, 0x12)
	require.NoError(t, err)
	lines := [][]byte{
		[]byte(fmt.Sprintf("VALUE key %d %d\r\n", flags, len(compressed))),
		append(append([]byte(nil), compressed...), '\r', '\n'),
		[]byte("END\r\n"),
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
			items, err := parseValueItems(lines, false, false, tt.codec)
			require.NoError(t, err)
			require.Len(t, items, 1)
			assert.Equal(t, &Item{Key: "key", Value: tt.value, Flags: tt.flags}, items[0])
		})
	}
}

func Test_buildRetrievalCommands(t *testing.T) {
	for _, tt := range []struct {
		command string
		expiry  time.Duration
		wantRaw string
	}{
		{command: "get", wantRaw: "get key1 key2\r\n"},
		{command: "gets", wantRaw: "gets key1 key2\r\n"},
		{command: "gat", expiry: time.Second, wantRaw: "gat 1 key1 key2\r\n"},
		{command: "gats", expiry: time.Second, wantRaw: "gats 1 key1 key2\r\n"},
	} {
		t.Run(tt.command, func(t *testing.T) {
			var req *request
			var resp *response
			if tt.expiry > 0 {
				req, resp = buildGetAndTouchesCommand(tt.command, tt.expiry, "key1", "key2")
			} else {
				req, resp = buildGetsCommand(tt.command, "key1", "key2")
			}
			defer releaseReqAndResp(req, resp)

			assert.Equal(t, tt.wantRaw, string(req.raw))
			assert.Equal(t, tt.command, string(req.cmd))
			assert.Equal(t, []byte("key1"), req.key)
			assert.Equal(t, endIndicatorSpecificEndLine, resp.endIndicator)
			assert.Equal(t, []byte("END\r\n"), resp.specEndLine)
			assert.Empty(t, resp.rawLines)
		})
	}
}

func Test_parseUintFromBytes(t *testing.T) {
	for _, tt := range []struct {
		raw     string
		want    uint64
		wantErr error
	}{
		{raw: "123", want: 123},
		{raw: "1234567890", want: 1234567890},
		{raw: ""},
		{raw: "abc", wantErr: ErrMalformedResponse},
		{raw: "1234567890abc", wantErr: ErrMalformedResponse},
		{raw: " ", wantErr: ErrMalformedResponse},
		{raw: "-123", wantErr: ErrMalformedResponse},
	} {
		t.Run(fmt.Sprintf("%q", tt.raw), func(t *testing.T) {
			got, err := parseUintFromBytes([]byte(tt.raw))
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func Test_parseValueLine(t *testing.T) {
	for _, tt := range []struct {
		name       string
		line       string
		withCAS    bool
		wantItem   Item
		wantLen    uint64
		wantErr    error
		errContext string
	}{
		{name: "without CAS", line: "VALUE mykey 123 456", wantItem: Item{Key: "mykey", Flags: 123}, wantLen: 456},
		{name: "with CAS", line: "VALUE mykey 123 456 789", withCAS: true, wantItem: Item{Key: "mykey", Flags: 123, CAS: 789}, wantLen: 456},
		{name: "invalid flags", line: "VALUE mykey abc 456", wantErr: ErrMalformedResponse, errContext: "invalid flags"},
		{name: "invalid data length", line: "VALUE mykey 123 abc ", wantErr: ErrMalformedResponse, errContext: "invalid data length"},
		{name: "invalid CAS", line: "VALUE mykey 123 456 abc", withCAS: true, wantErr: ErrMalformedResponse, errContext: "invalid CAS"},
		{name: "extra fields without CAS", line: "VALUE mykey 123 456 789 extra ", wantErr: ErrMalformedResponse, errContext: "invalid VALUE line"},
		{name: "extra fields with CAS", line: "VALUE mykey 123 456 789 extra more", withCAS: true, wantErr: ErrMalformedResponse, errContext: "invalid VALUE line"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			item := &Item{}
			gotLen, err := parseValueLine([]byte(tt.line), item, tt.withCAS)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				assert.Contains(t, err.Error(), tt.errContext)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantLen, gotLen)
			assert.Equal(t, tt.wantItem, *item)
		})
	}
}

func Test_parseStats(t *testing.T) {
	lines := [][]byte{
		[]byte("STAT version 1.5.12\r\n"),
		[]byte("STAT pid 12345\r\n"),
		[]byte("STAT uptime 123456\r\n"),
		[]byte("STAT time 1234567890\r\n"),
		[]byte("STAT pointer_size 64\r\n"),
		[]byte("STAT rusage_user 30440.595477\r\n"),
		[]byte("STAT rusage_system 41317.488860\r\n"),
		[]byte("STAT curr_connections 123\r\n"),
		[]byte("STAT total_connections 123456\r\n"),
		[]byte("STAT connection_structures 1234567890\r\n"),
		[]byte("STAT reserved_fds 1234567890\r\n"),
		[]byte("STAT accepting_conns 1\r\n"),
		[]byte("STAT hash_is_expanding 1\r\n"),
		[]byte("END\r\n"),
	}
	want := &Statistic{
		Version: "1.5.12", PID: 12345, Uptime: 123456, Time: 1234567890,
		PointerSize: 64, RusageUser: 30440.595477, RusageSystem: 41317.488860,
		CurrConnections: 123, TotalConnections: 123456, ConnectionStructures: 1234567890,
		ReservedFDs: 1234567890, AcceptingConns: true, HashIsExpanding: true,
	}
	got, err := parseStats(lines)
	require.NoError(t, err)
	assert.Equal(t, want, got)

	_, err = parseStats(nil)
	assert.ErrorIs(t, err, ErrMalformedResponse)
}

func Test_buildSimpleCommands(t *testing.T) {
	for _, tt := range []struct {
		name      string
		build     func() (*request, *response)
		wantRaw   string
		wantKey   string
		indicator responseEndIndicator
		endLine   string
	}{
		{
			name: "version", build: buildVersionCommand, wantRaw: "version\r\n",
			indicator: endIndicatorLimitedLines,
		},
		{
			name: "flush", build: func() (*request, *response) { return buildFlushAllCommand(false) },
			wantRaw: "flush_all\r\n", indicator: endIndicatorLimitedLines,
		},
		{
			name: "delete", build: func() (*request, *response) { return buildDeleteCommand("key", false) },
			wantRaw: "delete key\r\n", wantKey: "key", indicator: endIndicatorLimitedLines,
		},
		{
			name: "quiet delete", build: func() (*request, *response) { return buildDeleteCommand("key", true) },
			wantRaw: "delete key noreply\r\n", wantKey: "key", indicator: endIndicatorNoReply,
		},
		{
			name: "touch", build: func() (*request, *response) { return buildTouchCommand("key", 3*time.Second, false) },
			wantRaw: "touch key 3\r\n", wantKey: "key", indicator: endIndicatorLimitedLines,
		},
		{
			name: "quiet touch", build: func() (*request, *response) { return buildTouchCommand("key", 3*time.Second, true) },
			wantRaw: "touch key 3 noreply\r\n", wantKey: "key", indicator: endIndicatorNoReply,
		},
		{
			name: "increment", build: func() (*request, *response) { return buildArithmeticCommand("incr", "key", 42, false) },
			wantRaw: "incr key 42\r\n", wantKey: "key", indicator: endIndicatorLimitedLines,
		},
		{
			name: "quiet decrement", build: func() (*request, *response) { return buildArithmeticCommand("decr", "key", 42, true) },
			wantRaw: "decr key 42 noreply\r\n", wantKey: "key", indicator: endIndicatorNoReply,
		},
		{
			name: "stats", build: func() (*request, *response) { return buildStatsCommand("") },
			wantRaw: "stats\r\n", indicator: endIndicatorSpecificEndLine, endLine: "END\r\n",
		},
		{
			name: "stats subcommand", build: func() (*request, *response) { return buildStatsCommand("items") },
			wantRaw: "stats items\r\n", indicator: endIndicatorSpecificEndLine, endLine: "END\r\n",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			req, resp := tt.build()
			defer releaseReqAndResp(req, resp)

			assert.Equal(t, tt.wantRaw, string(req.raw))
			assert.Equal(t, tt.wantKey, string(req.key))
			assert.Equal(t, strings.Fields(tt.wantRaw)[0], string(req.cmd))
			assert.Equal(t, tt.indicator, resp.endIndicator)
			switch tt.indicator {
			case endIndicatorLimitedLines:
				assert.Equal(t, uint8(1), resp.limitedLines)
			case endIndicatorSpecificEndLine:
				assert.Equal(t, tt.endLine, string(resp.specEndLine))
			}
		})
	}
}

func Test_parseArithmetic(t *testing.T) {
	for _, tt := range []struct {
		name    string
		line    string
		want    uint64
		wantErr error
	}{
		{name: "quiet response"},
		{name: "value", line: "42\r\n", want: 42},
		{name: "maximum value", line: "18446744073709551615\r\n", want: ^uint64(0)},
		{name: "negative value", line: "-1\r\n", wantErr: strconv.ErrSyntax},
		{name: "overflow", line: "18446744073709551616\r\n", wantErr: strconv.ErrRange},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseArithmetic([]byte(tt.line))
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

type prependOnlyRestrictedCodec struct{}

func (prependOnlyRestrictedCodec) Encode(_ []byte, value []byte, flags uint32) ([]byte, uint32, error) {
	return value, flags, nil
}

func (prependOnlyRestrictedCodec) Decode(_ []byte, value []byte, flags uint32) ([]byte, uint32, error) {
	return value, flags, nil
}

func (prependOnlyRestrictedCodec) SupportsOperation(operation string) error {
	if operation == "prepend" || operation == "cas" {
		return ErrNotSupported
	}
	return nil
}

func TestCodecCapabilitiesApplyPerStorageOperation(t *testing.T) {
	codec := prependOnlyRestrictedCodec{}
	for _, operation := range []string{"append", "prepend", "cas", "meta append", "meta prepend"} {
		t.Run(operation, func(t *testing.T) {
			var req *request
			var resp *response
			var err error
			switch operation {
			case "append", "prepend":
				req, resp, err = buildStorageCommand(operation, "foo", []byte("bar"), 0, 0, false, codec)
			case "cas":
				req, resp, err = buildCasCommand("foo", []byte("bar"), 0, 0, 1, false, codec)
			default:
				mode := MetaSetModeAppend
				if operation == "meta prepend" {
					mode = MetaSetModePrepend
				}
				req, resp, err = buildMetaSetCommand([]byte("foo"), []byte("bar"), &metaSetFlags{M: mode}, codec)
			}
			if operation == "append" || operation == "meta append" {
				require.NoError(t, err)
				require.NotNil(t, req)
				require.NotNil(t, resp)
				releaseReqAndResp(req, resp)
			} else {
				require.ErrorIs(t, err, ErrNotSupported)
				require.Nil(t, req)
				require.Nil(t, resp)
			}
		})
	}
}
