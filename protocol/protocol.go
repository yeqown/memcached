package protocol

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
	spaceByte   = []byte(" ")
	newLineByte = []byte("\r\n")
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
	b.buf.WriteByte(' ')
	return b
}

func (b *protocolBuilder) AddInt(i int) *protocolBuilder {
	b.buf.WriteString(strconv.Itoa(i))
	b.buf.WriteByte(' ')
	return b
}

func (b *protocolBuilder) NewLine() *protocolBuilder {
	// trim space if needed
	if bytes.HasSuffix(b.buf.Bytes(), spaceByte) {
		b.buf.Truncate(b.buf.Len() - 1)
	}

	b.buf.WriteString("\r\n")
	return b
}

func (b *protocolBuilder) build() []byte {
	result := b.buf.Bytes()
	if bytes.HasSuffix(result, spaceByte) {
		result = result[:len(result)-1]
	}

	if bytes.HasSuffix(result, newLineByte) {
		return result
	}

	result = append(result, newLineByte...)
	return result
}
