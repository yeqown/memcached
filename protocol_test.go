package memcached

import (
	"bytes"
	"testing"

	"github.com/pkg/errors"
	"github.com/stretchr/testify/assert"
)

func Test_parseValueItems(t *testing.T) {
	type args struct {
		lines          [][]byte
		withoutEndLine bool
		withCAS        bool
	}
	tests := []struct {
		name    string
		args    args
		want    []*Item
		wantErr bool
	}{
		{
			name: "normal",
			args: args{
				lines: [][]byte{
					[]byte("VALUE key 0 5\r\n"),
					[]byte("value\r\n"),
					[]byte("VALUE key2 0 6\r\n"),
					[]byte("value2\r\n"),
					[]byte("END\r\n"),
				},
				withoutEndLine: false,
				withCAS:        false,
			},
			want: []*Item{
				{
					Key:   "key",
					Value: []byte("value"),
				},
				{
					Key:   "key2",
					Value: []byte("value2"),
				},
			},
			wantErr: false,
		},
		{
			name: "with flags/cas-unique",
			args: args{
				lines: [][]byte{
					[]byte("VALUE key 123 5 1\r\n"),
					[]byte("value\r\n"),
					[]byte("VALUE key2 123 6 2\r\n"),
					[]byte("value2\r\n"),
					[]byte("END\r\n"),
				},
				withoutEndLine: false,
				withCAS:        true,
			},
			want: []*Item{
				{
					Key:   "key",
					Value: []byte("value"),
					Flags: 123,
					CAS:   1,
				},
				{
					Key:   "key2",
					Value: []byte("value2"),
					Flags: 123,
					CAS:   2,
				},
			},
			wantErr: false,
		},
		{
			name: "without end line",
			args: args{
				lines: [][]byte{
					[]byte("VALUE key 123 5 1\r\n"),
					[]byte("value\r\n"),
					[]byte("VALUE key2 123 6 2\r\n"),
					[]byte("value2\r\n"),
				},
				withoutEndLine: true,
				withCAS:        true,
			},
			want: []*Item{
				{
					Key:   "key",
					Value: []byte("value"),
					Flags: 123,
					CAS:   1,
				},
				{
					Key:   "key2",
					Value: []byte("value2"),
					Flags: 123,
					CAS:   2,
				},
			},
			wantErr: false,
		},
		{
			name: "malformed-1",
			args: args{
				lines: [][]byte{
					[]byte("VALUE key -1 5\r\n"),
					[]byte("value\r\n"),
					[]byte("END\r\n"),
				},
				withoutEndLine: false,
				withCAS:        false,
			},
			want:    nil,
			wantErr: true,
		},
		{
			name: "malformed-2",
			args: args{
				lines: [][]byte{
					[]byte("VALUE key 0 5\r\n"),
				},
				withoutEndLine: true,
			},
			want:    nil,
			wantErr: true, // missing data block
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseValueItems(tt.args.lines, tt.args.withoutEndLine, tt.args.withCAS)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			assert.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func constructParts(raw []byte) [][]byte {
	return bytes.Split(trimCRLF(raw), []byte(" "))
}

func Test_parseUintFromBytes(t *testing.T) {
	type args struct {
		raw []byte
	}
	tests := []struct {
		name    string
		args    args
		want    uint64
		wantErr bool
	}{
		{
			name: "normal-1",
			args: args{
				raw: []byte("123"),
			},
			want:    123,
			wantErr: false,
		},
		{
			name: "normal-2",
			args: args{
				raw: []byte("1234567890"),
			},
			want:    1234567890,
			wantErr: false,
		},
		{
			name: "malformed-contains-letters",
			args: args{
				raw: []byte("abc"),
			},
			want:    0,
			wantErr: true,
		},
		{
			name: "malformed-contains-other-chars",
			args: args{
				raw: []byte("1234567890abc"),
			},
			want:    0,
			wantErr: true,
		},
		{
			name: "malformed-empty",
			args: args{
				raw: []byte(""),
			},
			want:    0,
			wantErr: false,
		},
		{
			name: "malformed-space",
			args: args{
				raw: []byte(" "),
			},
			want:    0,
			wantErr: true,
		},
		{
			name: "malformed-negative",
			args: args{
				raw: []byte("-123"),
			},
			want:    0,
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseUintFromBytes(tt.args.raw)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func Test_parseValueLine(t *testing.T) {
	tests := []struct {
		name       string
		line       []byte
		withCas    bool
		wantItem   *Item
		wantLen    uint64
		wantErr    bool
		wantErrMsg string
	}{
		{
			name:    "normal case without cas",
			line:    []byte("VALUE mykey 123 456"),
			withCas: false,
			wantItem: &Item{
				Key:   "mykey",
				Flags: 123,
				CAS:   0,
			},
			wantLen: 456,
			wantErr: false,
		},
		{
			name:    "normal case with cas",
			line:    []byte("VALUE mykey 123 456 789"),
			withCas: true,
			wantItem: &Item{
				Key:   "mykey",
				Flags: 123,
				CAS:   789,
			},
			wantLen: 456,
			wantErr: false,
		},
		{
			name:       "invalid flags",
			line:       []byte("VALUE mykey abc 456"),
			withCas:    false,
			wantItem:   &Item{Key: "mykey"},
			wantLen:    0,
			wantErr:    true,
			wantErrMsg: "invalid flags",
		},
		{
			name:       "invalid data length",
			line:       []byte("VALUE mykey 123 abc "),
			withCas:    false,
			wantItem:   &Item{Key: "mykey", Flags: 123},
			wantLen:    0,
			wantErr:    true,
			wantErrMsg: "invalid data length",
		},
		{
			name:       "too many fields without cas",
			line:       []byte("VALUE mykey 123 456 789 extra "),
			withCas:    false,
			wantItem:   &Item{},
			wantLen:    0,
			wantErr:    true,
			wantErrMsg: "invalid VALUE line",
		},
		{
			name:       "too many fields with cas",
			line:       []byte("VALUE mykey 123 456 789 extra more"),
			withCas:    true,
			wantItem:   &Item{},
			wantLen:    0,
			wantErr:    true,
			wantErrMsg: "invalid VALUE line",
		},
		{
			name:    "empty line",
			line:    []byte("VALUE "),
			withCas: false,
			wantItem: &Item{
				Key:   "",
				Flags: 0,
				CAS:   0,
			},
			wantLen: 0,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			item := &Item{}
			gotLen, err := parseValueLine(tt.line, item, tt.withCas)

			if tt.wantErr {
				assert.Error(t, err)
				assert.True(t, errors.Is(err, ErrMalformedResponse))
				assert.Contains(t, err.Error(), tt.wantErrMsg)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.wantLen, gotLen)
				assert.Equal(t, tt.wantItem.Key, item.Key)
				assert.Equal(t, tt.wantItem.Flags, item.Flags)
				assert.Equal(t, tt.wantItem.CAS, item.CAS)
			}
		})
	}
}

func Test_parseStats(t *testing.T) {
	type args struct {
		lines [][]byte
	}
	tests := []struct {
		name    string
		args    args
		want    *Statistic
		wantErr bool
	}{
		{
			name: "normal case",
			args: args{
				lines: [][]byte{
					[]byte("STAT version 1.5.12"),
					[]byte("STAT pid 12345"),
					[]byte("STAT uptime 123456"),
					[]byte("STAT time 1234567890"),
					[]byte("STAT pointer_size 64"),
					[]byte("STAT rusage_user 30440.595477"),
					[]byte("STAT rusage_system 41317.488860"),
					[]byte("STAT curr_connections 123"),
					[]byte("STAT total_connections 123456"),
					[]byte("STAT connection_structures 1234567890"),
					[]byte("STAT reserved_fds 1234567890"),
					[]byte("STAT accepting_conns 1"),
					[]byte("STAT hash_is_expanding 1"),
				},
			},
			want: &Statistic{
				Version:              "1.5.12",
				PID:                  12345,
				Uptime:               123456,
				Time:                 1234567890,
				PointerSize:          64,
				RusageUser:           30440.595477,
				RusageSystem:         41317.488860,
				CurrConnections:      123,
				TotalConnections:     123456,
				ConnectionStructures: 1234567890,
				ReservedFDs:          1234567890,
				AcceptingConns:       true,
				HashIsExpanding:      true,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseStats(tt.args.lines)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			assert.Equalf(t, tt.want, got, "parseStats(%v)", tt.args.lines)
		})
	}
}
