package memcached

import (
	"bytes"
	"strconv"
)

// The protocolBuilder is used to build a protocol message.
// We can use it build request command fastly with chaining method like this:
//
// newProtocolBuilder().
//   AddString("set").AddString("key").AddInt(0).AddInt(0).AddInt(5).NewLine().
//   AddString("value").build()
//
// The result is:
//
// set key 0 0 5\r\n
// value\r\n
//

var (
	_SpaceBytes   = []byte{' '}
	_SpaceByte    = byte(' ')
	_CRLFBytes    = []byte("\r\n")
	_NoReplyBytes = []byte("noreply")

	_ValueBytes      = []byte("VALUE")
	_EndBytes        = []byte("END")
	_EndCRLFBytes    = []byte("END\r\n")
	_StoredBytes     = []byte("STORED")
	_StoredCRLFBytes = []byte("STORED\r\n")
	_VersionBytes    = []byte("VERSION")
)

type protocolBuilder struct {
	buf bytes.Buffer
}

func newProtocolBuilder() *protocolBuilder {
	return &protocolBuilder{
		buf: bytes.Buffer{},
	}
}

func (b *protocolBuilder) AddString(s string) *protocolBuilder {
	b.buf.WriteString(s)
	b.buf.WriteByte(_SpaceByte)
	return b
}

func (b *protocolBuilder) AddBytes(bs []byte) *protocolBuilder {
	b.buf.Write(bs)
	b.buf.WriteByte(_SpaceByte)
	return b
}

func (b *protocolBuilder) AddInt(i int) *protocolBuilder {
	b.buf.WriteString(strconv.Itoa(i))
	b.buf.WriteByte(_SpaceByte)
	return b
}

func (b *protocolBuilder) AddUint(i uint64) *protocolBuilder {
	b.buf.WriteString(strconv.FormatUint(i, 10))
	b.buf.WriteByte(_SpaceByte)
	return b
}

func (b *protocolBuilder) AddCRLF() *protocolBuilder {
	// trim space if needed
	if bytes.HasSuffix(b.buf.Bytes(), _SpaceBytes) {
		b.buf.Truncate(b.buf.Len() - 1)
	}

	b.buf.Write(_CRLFBytes)
	return b
}

func (b *protocolBuilder) build() []byte {
	result := b.buf.Bytes()
	if bytes.HasSuffix(result, _SpaceBytes) {
		result = result[:len(result)-1]
	}

	if bytes.HasSuffix(result, _CRLFBytes) {
		return result
	}

	result = append(result, _CRLFBytes...)
	return result
}

func trimCRLF(line []byte) []byte {
	return bytes.TrimSuffix(line, _CRLFBytes)
}

func withCRLF(bs []byte) []byte {
	return append(bs, _CRLFBytes...)
}
