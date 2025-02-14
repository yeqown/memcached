package memcached

import (
	"bytes"
	"io"
	"testing"

	"github.com/pkg/errors"
	"github.com/stretchr/testify/assert"
)

func Test_binaryRequest_send(t *testing.T) {
	tests := []struct {
		name      string
		binaryReq *binaryRequest
		wantError bool
		wantRaw   []byte
	}{
		{
			name: "case1: success",
			binaryReq: &binaryRequest{
				opcode: _binaryOpcodeSASLAuth,
				opaque: 123,
				cas:    123,
				extras: []byte("extra"),
				key:    []byte("key"),
				value:  []byte("value"),
			},
			wantError: false,
			wantRaw: []byte{
				0x80, 0x20, 0x0, 0x3, // magic(0x80), opcode(0x21), key length(0x3)
				0x5, 0x0, 0x0, 0x0, // extras length(0x5), data type(0x0), vbucket id(0x0)
				0x0, 0x0, 0x0, 0xd, // total body length(0xd),
				0x0, 0x0, 0x0, 0x7b, // opaque(0x7b) 123

				0x0, 0x0, 0x0, 0x0,
				0x0, 0x0, 0x0, 0x7b, // cas(0x7b) 123

				0x65, 0x78, 0x74, 0x72, 0x61, // extras: extra
				0x6b, 0x65, 0x79, // key: key
				0x76, 0x61, 0x6c, 0x75, 0x65, // value: value
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := &bytes.Buffer{}
			err := tt.binaryReq.send(w)
			if tt.wantError {
				assert.Error(t, err, "send(%v)", w)
				return
			}

			assert.Equalf(t, tt.wantRaw, w.Bytes(), "send(%v)", w)
		})
	}
}

func Test_binaryResponse_read(t *testing.T) {
	type args struct {
		sourceRaw []byte
	}
	tests := []struct {
		name        string
		args        args
		wantErr     bool
		wantSpecErr error
		want        *binaryResponse
	}{
		{
			name: "case1: full set and status ok",
			args: args{
				sourceRaw: []byte{
					0x81, 0x20, 0x0, 0x3, // magic(0x81), opcode(0x20), key length(0x3)
					0x5, 0x0, 0x0, 0x0, // extras length(0x5), data type(0x0), status: 0x0
					0x0, 0x0, 0x0, 0xd, // total body length(0xd),
					0x0, 0x0, 0x0, 0x7b, // opaque(0x7b) 123
					0x0, 0x0, 0x0, 0x0, // cas(0x0) 0
					0x0, 0x0, 0x0, 0x7b, // cas(0x7b) 123
					0x65, 0x78, 0x74, 0x72, 0x61, // extras: extra
					0x6b, 0x65, 0x79, // key: key
					0x76, 0x61, 0x6c, 0x75, 0x65, // value: value
				},
			},
			wantErr: false,
			want: &binaryResponse{
				opcode:          _binaryOpcodeSASLAuth,
				keyLength:       3,
				extrasLength:    5,
				status:          _binaryStatusOK,
				totalBodyLength: 13,
				opaque:          123,
				cas:             123,
				extras:          []byte("extra"),
				key:             []byte("key"),
				value:           []byte("value"),
			},
		},
		{
			name: "case2: invalid magic",
			args: args{
				sourceRaw: []byte{
					0x1b, 0x21, 0x0, 0x3, // magic(0x1b), opcode(0x21), key length(0x3)
					0x5, 0x0, 0x0, 0x0, // extras length(0x5), data type(0x0), status: 0x0
					0x0, 0x0, 0x0, 0xd, // total body length(0xd),
					0x0, 0x0, 0x0, 0x7b, // opaque(0x7b) 123
					0x0, 0x0, 0x0, 0x0,
					0x0, 0x0, 0x0, 0x7b, // cas(0x7b) 123
					0x65, 0x78, 0x74, 0x72, 0x61, // extras: extra
					0x6b, 0x65, 0x79, // key: key
					0x76, 0x61, 0x6c, 0x75, 0x65, // value: value
				},
			},
			wantErr:     true,
			wantSpecErr: ErrInvalidBinaryProtocol,
		},
		{
			name: "case3: malformed response: lack of body",
			args: args{
				sourceRaw: []byte{
					0x81, 0x21, 0x0, 0x3, // magic(0x81), opcode(0x21), key length(0x3)
					0x5, 0x0, 0x0, 0x0, // extras length(0x5), data type(0x0), status: 0x0
					0x0, 0x0, 0x0, 0xd, // total body length(0xd),
					0x0, 0x0, 0x0, 0x7b, // opaque(0x7b) 123
					0x0, 0x0, 0x0, 0x0,
					0x0, 0x0, 0x0, 0x7b, // cas(0x7b) 123
					0x65, 0x78, 0x74, 0x72, 0x61, // extras: extra
					0x6b, 0x65, 0x79, // key: key
					// lack of value
				},
			},
			wantErr:     true,
			wantSpecErr: io.ErrUnexpectedEOF,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := &binaryResponse{}
			err := resp.read(bytes.NewReader(tt.args.sourceRaw))
			if tt.wantErr {
				assert.Error(t, err, "read(%v)", tt.args.sourceRaw)
				assert.True(t, errors.Is(err, tt.wantSpecErr), "got error: %v", err)
				return
			}

			assert.Equal(t, tt.want, resp)
			assert.NoError(t, err, "read(%v)", tt.args.sourceRaw)
		})
	}
}
