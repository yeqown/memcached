package memcached

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_parseValueItems(t *testing.T) {
	type args struct {
		lines          [][]byte
		withoutEndLine bool
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
			got, err := parseValueItems(tt.args.lines, tt.args.withoutEndLine)
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

func Test_parseFlags(t *testing.T) {

	type args struct {
		parts    [][]byte
		startPos int
		item     *MetaItem
	}
	tests := []struct {
		name string
		args args
		want *MetaItem
	}{
		{
			name: "normal",
			args: args{
				parts:    constructParts([]byte("HD c26 kZm9v b O456 s3\r\n")),
				startPos: 1,
				item:     &MetaItem{},
			},
			want: &MetaItem{
				//Key:              []byte("foo"),
				Value:            nil,
				CAS:              26,
				Flags:            0,
				TTL:              0,
				LastAccessedTime: 0,
				Size:             3,
				Opaque:           456,
				HitBefore:        false,
			},
		},
		{
			name: "with flags",
			args: args{
				parts:    constructParts([]byte("VA 3 c29 f123 h1 kZm9v b l0 O789 s3 t200\r\n")),
				startPos: 2,
				item:     &MetaItem{},
			},
			want: &MetaItem{
				//Key:              []byte("foo"),
				Value:            nil,
				CAS:              29,
				Flags:            123,
				TTL:              200,
				LastAccessedTime: 0,
				Size:             3,
				Opaque:           789,
				HitBefore:        true,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parseFlags(tt.args.parts, tt.args.startPos, tt.args.item)
			assert.Equal(t, tt.want, tt.args.item)
		})
	}
}

func Test_parseMetaItem(t *testing.T) {
	type args struct {
		lines   [][]byte
		item    *MetaItem
		noReply bool
	}
	tests := []struct {
		name     string
		args     args
		wantErr  bool
		wantItem *MetaItem
	}{
		{
			name: "normal1: not found error and noReply false",
			args: args{
				lines: [][]byte{
					//ERROR
					[]byte("NF\r\n"),
				},
				item:    &MetaItem{},
				noReply: false,
			},
			wantErr:  true,
			wantItem: nil,
		},
		{
			name: "normal2: miss error and noReply true",
			args: args{
				lines: [][]byte{
					//ERROR
					[]byte("EN\r\n"),
				},
				item:    &MetaItem{},
				noReply: true,
			},
			wantErr:  true,
			wantItem: nil,
		},
		{
			name: "normal3: HD",
			args: args{
				lines: [][]byte{
					//HD c26 kZm9v b O456 s3
					[]byte("HD c26 kZm9v b O456 s3\r\n"),
				},
				item:    &MetaItem{},
				noReply: false,
			},
			wantErr: false,
			wantItem: &MetaItem{
				//Key:              []byte("foo"),
				Value:            nil,
				CAS:              26,
				Flags:            0,
				TTL:              0,
				LastAccessedTime: 0,
				Size:             3,
				Opaque:           456,
				HitBefore:        false,
			},
		},
		{
			name: "normal4: VA",
			args: args{
				lines: [][]byte{
					//VA 3 c29 f123 h1 kZm9v b l0 O789 s3 t200
					//bar
					[]byte("VA 3 c29 f123 h1 kZm9v b l0 O789 s3 t200\r\n"),
					[]byte("bar\r\n"),
				},
				item:    &MetaItem{},
				noReply: false,
			},
			wantErr: false,
			wantItem: &MetaItem{
				//Key:              []byte("foo"), // key is not set in parseMetaItem
				Value:            []byte("bar"),
				CAS:              29,
				Flags:            123,
				TTL:              200,
				LastAccessedTime: 0,
				Size:             3,
				Opaque:           789,
				HitBefore:        true,
			},
		},
		{
			name: "malformed1: missing data block",
			args: args{
				lines: [][]byte{
					//VA 3 c29 f123 h1 kZm9v b l0 O789 s3 t200
					[]byte("VA 3 c29 f123 h1 kZm9v b l0 O789 s3 t200\r\n"),
				},
				item:    &MetaItem{},
				noReply: false,
			},
			wantErr:  true,
			wantItem: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := parseMetaItem(tt.args.lines, tt.args.item, tt.args.noReply)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			assert.NoError(t, err)
			assert.Equal(t, tt.wantItem, tt.args.item)
		})
	}
}
