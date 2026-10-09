package memcached

import (
	"bytes"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_binaryRequest_send(t *testing.T) {
	req := &binaryRequest{
		opcode: _binaryOpcodeSASLAuth, opaque: 123, cas: 123,
		extras: []byte("extra"), key: []byte("key"), value: []byte("value"),
	}
	wantRaw := []byte{
		0x80, 0x21, 0x00, 0x03, // magic, opcode, key length
		0x05, 0x00, 0x00, 0x00, // extras length, data type, vbucket
		0x00, 0x00, 0x00, 0x0d, // body length
		0x00, 0x00, 0x00, 0x7b, // opaque
		0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x7b, // CAS
		0x65, 0x78, 0x74, 0x72, 0x61, // extras: extra
		0x6b, 0x65, 0x79, // key: key
		0x76, 0x61, 0x6c, 0x75, 0x65, // value: value
	}

	var w bytes.Buffer
	require.NoError(t, req.send(&w))
	assert.Equal(t, wantRaw, w.Bytes())

	cn := newTestConn()
	cn.writeErr = io.ErrClosedPipe
	assert.ErrorIs(t, req.send(cn), io.ErrClosedPipe)
}

func Test_binaryResponse_read(t *testing.T) {
	raw := []byte{
		0x81, 0x20, 0x00, 0x03, // magic, opcode, key length
		0x05, 0x00, 0x00, 0x00, // extras length, data type, status
		0x00, 0x00, 0x00, 0x0d, // body length
		0x00, 0x00, 0x00, 0x7b, // opaque
		0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x7b, // CAS
		0x65, 0x78, 0x74, 0x72, 0x61, // extras: extra
		0x6b, 0x65, 0x79, // key: key
		0x76, 0x61, 0x6c, 0x75, 0x65, // value: value
	}
	invalidMagic := append([]byte(nil), raw...)
	invalidMagic[0] = 0x1b

	for _, tt := range []struct {
		name    string
		raw     []byte
		want    *binaryResponse
		wantErr error
	}{
		{
			name: "full response", raw: raw,
			want: &binaryResponse{
				opcode: _binaryOpcodeSASLListMechanisms, keyLength: 3, extrasLength: 5,
				status: _binaryStatusOK, totalBodyLength: 13, opaque: 123, cas: 123,
				extras: []byte("extra"), key: []byte("key"), value: []byte("value"),
			},
		},
		{name: "invalid magic", raw: invalidMagic, wantErr: ErrInvalidBinaryProtocol},
		{name: "truncated body", raw: raw[:len(raw)-5], wantErr: io.ErrUnexpectedEOF},
		{name: "truncated header", raw: raw[:23], wantErr: io.ErrUnexpectedEOF},
		{name: "empty response", wantErr: io.EOF},
	} {
		t.Run(tt.name, func(t *testing.T) {
			resp := &binaryResponse{}
			err := resp.read(bytes.NewReader(tt.raw))
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, resp)
		})
	}
}

func Test_binaryResponse_status(t *testing.T) {
	for _, tt := range []struct {
		name    string
		status  uint16
		opcode  uint8
		wantErr error
	}{
		{name: "success", status: _binaryStatusOK},
		{name: "auth continuation", status: _binaryStatusAuthContinue, wantErr: ErrAuthenticationFailed},
		{name: "auth error", status: _binaryStatusAuthError, wantErr: ErrAuthenticationFailed},
		{name: "authentication failed", status: _binaryStatusAuthenticationFailed, wantErr: ErrAuthenticationFailed},
		{name: "unknown SASL list", status: _binaryStatusUnknownCmd, opcode: _binaryOpcodeSASLListMechanisms, wantErr: ErrAuthenticationUnSupported},
		{name: "unknown SASL auth", status: _binaryStatusUnknownCmd, opcode: _binaryOpcodeSASLAuth, wantErr: ErrAuthenticationUnSupported},
		{name: "unknown SASL step", status: _binaryStatusUnknownCmd, opcode: _binaryOpcodeSASLStep, wantErr: ErrAuthenticationUnSupported},
		{name: "unknown command", status: _binaryStatusUnknownCmd, wantErr: ErrNonexistentCommand},
		{name: "not supported", status: _binaryStatusNotSupported, wantErr: ErrNotSupported},
		{name: "internal error", status: _binaryStatusInternalError, wantErr: ErrServerError},
		{name: "invalid arguments", status: _binaryStatusInvalidArgs, wantErr: ErrInvalidArgument},
		{name: "out of memory", status: _binaryStatusOutOfMemory, wantErr: ErrServerError},
		{name: "unknown status", status: 0xbeef, wantErr: ErrServerError},
	} {
		t.Run(tt.name, func(t *testing.T) {
			resp := &binaryResponse{status: tt.status, opcode: tt.opcode}
			err := resp.expect(_binaryStatusOK)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
		})
	}
}

func Test_binarySASLCommands(t *testing.T) {
	t.Run("list mechanisms", func(t *testing.T) {
		req, _ := saslListMechanisms()
		var w bytes.Buffer
		require.NoError(t, req.send(&w))
		assert.Equal(t, []byte{
			0x80, 0x20, 0, 0, 0, 0, 0, 0,
			0, 0, 0, 0, 0, 0, 0, 0,
			0, 0, 0, 0, 0, 0, 0, 0,
		}, w.Bytes())
	})

	t.Run("PLAIN credentials", func(t *testing.T) {
		req, _ := saslAuthRequestPlain("user", "secret")
		var w bytes.Buffer
		require.NoError(t, req.send(&w))
		want := append([]byte{
			0x80, 0x21, 0, 5, 0, 0, 0, 0,
			0, 0, 0, 17, 0, 0, 0, 0,
			0, 0, 0, 0, 0, 0, 0, 0,
		}, []byte("PLAIN\x00user\x00secret")...)
		assert.Equal(t, want, w.Bytes())
	})
}
