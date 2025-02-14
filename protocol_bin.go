package memcached

import (
	"encoding/binary"
	"io"

	"github.com/pkg/errors"
)

// https://docs.memcached.org/protocols/binary/
//
// General packet format:
//
//	Byte/     0       |       1       |       2       |       3       |
//	   /              |               |               |               |
//	  |0 1 2 3 4 5 6 7|0 1 2 3 4 5 6 7|0 1 2 3 4 5 6 7|0 1 2 3 4 5 6 7|
//	  +---------------+---------------+---------------+---------------+
//	 0/ HEADER                                                        /
//	  /                                                               /
//	  /                                                               /
//	  /                                                               /
//	  +---------------+---------------+---------------+---------------+
//	24/ COMMAND-SPECIFIC EXTRAS (as needed)                           /
//	 +/  (note length in the extras length header field)              /
//	  +---------------+---------------+---------------+---------------+
//	 m/ Key (as needed)                                               /
//	 +/  (note length in key length header field)                     /
//	  +---------------+---------------+---------------+---------------+
//	 n/ Value (as needed)                                             /
//	 +/  (note length is total body length header field, minus        /
//	 +/   sum of the extras and key length body fields)               /
//	  +---------------+---------------+---------------+---------------+
//	  Total 24 bytes
//
// Request Header
//
//	Byte/     0       |       1       |       2       |       3       |
//	   /              |               |               |               |
//	  |0 1 2 3 4 5 6 7|0 1 2 3 4 5 6 7|0 1 2 3 4 5 6 7|0 1 2 3 4 5 6 7|
//	  +---------------+---------------+---------------+---------------+
//	 0| Magic         | Opcode        | Key length                    |
//	  +---------------+---------------+---------------+---------------+
//	 4| Extras length | Data type     | vbucket id                    |
//	  +---------------+---------------+---------------+---------------+
//	 8| Total body length                                             |
//	  +---------------+---------------+---------------+---------------+
//	12| Opaque                                                        |
//	  +---------------+---------------+---------------+---------------+
//	16| CAS                                                           |
//	  |                                                               |
//	  +---------------+---------------+---------------+---------------+
//	  Total 24 bytes
//
// Response Header:
//
//	Byte/     0       |       1       |       2       |       3       |
//	   /              |               |               |               |
//	  |0 1 2 3 4 5 6 7|0 1 2 3 4 5 6 7|0 1 2 3 4 5 6 7|0 1 2 3 4 5 6 7|
//	  +---------------+---------------+---------------+---------------+
//	 0| Magic         | Opcode        | Key Length                    |
//	  +---------------+---------------+---------------+---------------+
//	 4| Extras length | Data type     | Status                        |
//	  +---------------+---------------+---------------+---------------+
//	 8| Total body length                                             |
//	  +---------------+---------------+---------------+---------------+
//	12| Opaque                                                        |
//	  +---------------+---------------+---------------+---------------+
//	16| CAS                                                           |
//	  |                                                               |
//	  +---------------+---------------+---------------+---------------+
//	  Total 24 bytes

const (
	_binaryMagicReq = 0x80
	_binaryMagicRes = 0x81

	_binaryStatusOK                   = 0x0000 // No error
	_binaryStatusKeyNotFound          = 0x0001 // Key not found
	_binaryStatusKeyExists            = 0x0002 // Key exists
	_binaryStatusValueTooBig          = 0x0003 // Value too large
	_binaryStatusInvalidArgs          = 0x0004 // Invalid arguments
	_binaryStatusItemNotStored        = 0x0005 // Item not stored
	_binaryStatusNonNumeric           = 0x0006 // Incr/Decr on non-numeric value
	_binaryStatusVBucketBelong        = 0x0007 // The vbucket belongs to another server
	_binaryStatusAuthError            = 0x0008 // Authentication error
	_binaryStatusAuthContinue         = 0x0009 // Authentication continue
	_binaryStatusAuthenticationFailed = 0x0020 // Authentication failed
	_binaryStatusUnknownCmd           = 0x0081 // Unknown command
	_binaryStatusOutOfMemory          = 0x0082 // Out of memory
	_binaryStatusNotSupported         = 0x0083 // Not supported
	_binaryStatusInternalError        = 0x0084 // Internal error
	_binaryStatusBusy                 = 0x0085 // Busy
	_binaryStatusTmpFailure           = 0x0086 // Temporary failure

	/**
	 * Opcodes possible in binary protocol:
	 * only list SASL related opcode here, since binary protocol is not used in
	 * normal operation.
	 */

	_binaryOpcodeSASLAuth = 0x20 // SASL authentication
	_binaryOpcodeSASLStep = 0x21 // SASL authentication continue

	/**
	 * Data types in binary protocol
	 */
	_binaryDataTypeRawBytes = 0x00 // Raw bytes
)

type binaryRequest struct {
	// magic        uint8 // must be 0x80
	opcode uint8
	// keyLength    uint16 // calculated by len(key)
	// extrasLength uint8 // calculated by len(extras)
	// dataType     uint8 // always 0x00 (Raw bytes)
	// vbucketId    uint16 // always 0
	// bodyLength   uint32 // calculated by len(key) + len(value) + len(extras)
	opaque uint32
	cas    uint64

	extras []byte
	key    []byte
	value  []byte
}

func (br *binaryRequest) send(w io.Writer) error {
	nKey := uint16(len(br.key))
	nValue := uint32(len(br.value))
	nExtras := uint8(len(br.extras))

	buf := make([]byte, 24+uint32(nExtras)+uint32(nKey)+nValue)

	buf[0] = _binaryMagicReq
	buf[1] = br.opcode
	binary.BigEndian.PutUint16(buf[2:4], nKey)                                 // key length
	buf[4] = nExtras                                                           // extras length
	buf[5] = _binaryDataTypeRawBytes                                           // data type
	binary.BigEndian.PutUint16(buf[6:8], uint16(0))                            // vbucket id
	binary.BigEndian.PutUint32(buf[8:12], uint32(nKey)+nValue+uint32(nExtras)) // total body length
	binary.BigEndian.PutUint32(buf[12:16], br.opaque)                          // opaque
	binary.BigEndian.PutUint64(buf[16:24], br.cas)                             // cas

	s := uint32(24)
	if nExtras > 0 {
		copy(buf[s:s+uint32(nExtras)], br.extras)
		s += uint32(nExtras)
	}

	if nKey > 0 {
		copy(buf[s:s+uint32(nKey)], br.key)
		s += uint32(nKey)
	}
	if nValue > 0 {
		copy(buf[s:], br.value)
	}

	_, err := w.Write(buf)
	return err
}

type binaryResponse struct {
	// magic        uint8 // must be 0x81
	opcode       uint8
	keyLength    uint16
	extrasLength uint8
	// dataType     uint8 // always 0x00 (Raw bytes)
	status          uint16
	totalBodyLength uint32
	opaque          uint32
	cas             uint64

	extras []byte
	key    []byte
	value  []byte
}

func (br *binaryResponse) hasError() error {
	switch br.status {
	case _binaryStatusOK, _binaryStatusAuthContinue:
		return nil
	case _binaryStatusAuthError, _binaryStatusAuthenticationFailed:
		return ErrAuthenticationFailed
	case _binaryStatusUnknownCmd:
		return ErrNonexistentCommand
	case _binaryStatusNotSupported:
		return ErrNotSupported
	case _binaryStatusInternalError:
		return errors.Wrap(ErrServerError, "internal error")
	case _binaryStatusInvalidArgs:
		return ErrInvalidArgument
	case _binaryStatusOutOfMemory:
		return errors.Wrap(ErrServerError, "out of memory")
	}

	// return: status: 0x1234 format
	return errors.Wrapf(ErrServerError, "status: %x", br.status)
}

func (br *binaryResponse) read(rr io.Reader) error {
	buf := make([]byte, 24)
	if _, err := io.ReadFull(rr, buf); err != nil {
		return errors.Wrap(err, "read header")
	}

	if magic := buf[0]; magic != _binaryMagicRes {
		return errors.Wrapf(ErrInvalidBinaryProtocol, "invalid magic: %d", magic)
	}

	br.opcode = buf[1]
	br.keyLength = binary.BigEndian.Uint16(buf[2:4])
	br.extrasLength = buf[4]
	br.status = binary.BigEndian.Uint16(buf[6:8])
	br.totalBodyLength = binary.BigEndian.Uint32(buf[8:12])
	br.opaque = binary.BigEndian.Uint32(buf[12:16])
	br.cas = binary.BigEndian.Uint64(buf[16:24])

	if br.totalBodyLength == 0 {
		// no extras, key, value
		return nil
	}

	// read the whole body and split them into extras, key, value
	body := make([]byte, br.totalBodyLength)
	if _, err := io.ReadFull(rr, body); err != nil {
		return errors.Wrap(err, "read body")
	}

	s := uint32(0)
	if br.extrasLength > 0 {
		br.extras = body[:br.extrasLength]
		s += uint32(br.extrasLength)
	}
	if br.keyLength > 0 {
		br.key = body[s : s+uint32(br.keyLength)]
		s += uint32(br.keyLength)
	}

	nValue := br.totalBodyLength - uint32(br.extrasLength) - uint32(br.keyLength)
	if nValue > 0 {
		br.value = body[s:]
	}

	return nil
}

func buildAuthListMechanisms() (*binaryRequest, *binaryResponse) {
	req := &binaryRequest{
		opcode: _binaryOpcodeSASLAuth,
	}

	resp := &binaryResponse{}
	return req, resp
}
