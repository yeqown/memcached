package memcached

import (
	"bytes"
	"encoding/base64"
	"math"
	"strconv"

	"github.com/pkg/errors"
)

const (
	maxKeySize   = math.MaxUint16
	maxValueSize = math.MaxUint32
)

var (
	_SpaceBytes    = []byte{' '}
	_SpaceByte     = byte(' ')
	_CRLFBytes     = []byte("\r\n")
	_NoReplyBytes  = []byte("noreply")
	_QuitCRLFBytes = []byte("quit\r\n")

	_OKCRLFBytes      = []byte("OK\r\n")
	_ValueBytes       = []byte("VALUE")
	_EndBytes         = []byte("END")
	_EndCRLFBytes     = []byte("END\r\n")
	_StoredBytes      = []byte("STORED")
	_StoredCRLFBytes  = []byte("STORED\r\n")
	_DeletedBytes     = []byte("DELETED")
	_DeletedCRLFBytes = []byte("DELETED\r\n")
	_TouchedCRLFBytes = []byte("TOUCHED\r\n")
	_VersionBytes     = []byte("VERSION")

	_MetaEndCRLFBytes = []byte("EN\r\n")
)

// forecastCommonFaultLine forecasts the error line from the response line.
// ERROR\r\n
// CLIENT_ERROR <message>\r\n
// SERVER_ERROR <message>\r\n
// NOT_FOUND\r\n
// EXISTS\r\n
// NOT_STORED\r\n
func forecastCommonFaultLine(line []byte) error {
	switch {
	case bytes.Equal(line, []byte("ERROR\r\n")):
		return ErrNonexistentCommand
	case bytes.HasPrefix(line, []byte("CLIENT_ERROR")):
		message := string(line[12 : len(line)-2])
		return errors.Wrap(ErrClientError, message)
	case bytes.HasPrefix(line, []byte("SERVER_ERROR")):
		message := string(line[12 : len(line)-2])
		return errors.Wrap(ErrServerError, message)
	case bytes.Equal(line, []byte("NOT_FOUND\r\n")):
		return ErrNotFound
	case bytes.Equal(line, []byte("EXISTS\r\n")):
		return ErrExists
	case bytes.Equal(line, []byte("NOT_STORED\r\n")):
		return ErrNotStored
	}

	return nil
}

// The protocolBuilder is used to build a protocol message.
// We can use it build request command quickly with chaining method like this:
//
// newProtocolBuilder().
// AddString("set").AddString("key").AddInt(0).AddInt(0).AddInt(5).NewLine().
// AddString("value").build()
//
// The result is:
//
// set key 0 0 5\r\n
// value\r\n
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

// AddFlagBool adds a flag with string value to the protocol message.
// e.g. AddFlagBool("k", true) will append "k" to the protocol message.
func (b *protocolBuilder) AddFlagBool(flag string, v bool) *protocolBuilder {
	if !v {
		return b
	}

	b.buf.WriteString(flag)
	b.buf.WriteByte(_SpaceByte)
	return b
}

// AddFlagUint adds a flag with uint64 value to the protocol message.
// e.g. AddFlagUint("c", 1) will append "c1 " to the protocol message.
func (b *protocolBuilder) AddFlagUint(flag string, tok uint64) *protocolBuilder {
	if tok == 0 {
		return b
	}

	b.buf.WriteString(flag + strconv.FormatUint(tok, 10))
	b.buf.WriteByte(_SpaceByte)
	return b
}

func (b *protocolBuilder) AddFlagString(flag, tok string) *protocolBuilder {
	if tok == "" {
		return b
	}

	b.buf.WriteString(flag + tok)
	b.buf.WriteByte(_SpaceByte)
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

type request struct {
	cmd []byte // command name
	key []byte // key is nil if the command DOES NOT need key
	raw []byte
}

func (req *request) send(rr memcachedConn) (err error) {
	_, err = rr.Write(req.raw)
	return err
}

type responseEndIndicator uint8

const (
	// endIndicatorNoReply indicates the response is no reply
	// and the client should not wait for the response.
	endIndicatorNoReply responseEndIndicator = iota
	// endIndicatorLimitedLines indicates the response is limited lines,
	// the client should read line from response with limited lines with delimiter '\n'.
	endIndicatorLimitedLines
	// endIndicatorSpecificEndLine indicates the response is specific end line,
	// the client should read lines from response until the specific end line.
	// The delimiter is '\n'.
	endIndicatorSpecificEndLine
)

// response represents a structural response from memcached server.
type response struct {
	// endIndicator indicates the parser how to read the whole bytes from the
	// connection receiving buffer.
	endIndicator responseEndIndicator
	// limitedLines is the number of lines to read from the connection.
	// If limitedLines equals 0, it means the response is not ready to be read
	// from the connection. 1 means the response is
	// ready to be read from the connection.
	limitedLines uint8
	// specEndLine is the specific end line of the response, it helps to read
	// from the connection.
	specEndLine []byte

	// rawLines is the raw bytes of the response, it has been divided by '\n'.
	// .e.g. "VALUE key 0 5\r\nvalue\r\nEND\r\n" will be divided into
	// ["VALUE key 0 5\r\n", "value\r\n", "END\r\n"].
	rawLines [][]byte
}

func (resp *response) recv(rr memcachedConn) error {
	switch resp.endIndicator {
	case endIndicatorNoReply:
		return nil
	case endIndicatorLimitedLines:
		return resp.read1(rr)
	case endIndicatorSpecificEndLine:
		return resp.read2(rr)
	}

	return ErrUnknownIndicator
}

// read1 reads the response from the connection with limited lines.
func (resp *response) read1(rr memcachedConn) error {
	read := 0
	for read < int(resp.limitedLines) {
		line, err := rr.readLine('\n')
		if err != nil {
			return errors.Wrap(err, "dispatchRequest read")
		}

		if read == 0 {
			if err = forecastCommonFaultLine(line); err != nil {
				return err
			}
		}

		resp.rawLines = append(resp.rawLines, line)
		read++
	}

	return nil
}

// read2 reads the response from the connection with specific end line.
func (resp *response) read2(rr memcachedConn) error {
	for {
		// FIXME(@yeqown): read line would cost too much capacity.
		line, err := rr.readLine('\n')
		if err != nil {
			return errors.Wrap(err, "dispatchRequest read")
		}

		// FIXED(@yeqown): The end line also should be added to the rawLines.
		if bytes.Equal(line, resp.specEndLine) {
			resp.rawLines = append(resp.rawLines, line)
			break
		}

		if err = forecastCommonFaultLine(line); err != nil {
			return err
		}

		resp.rawLines = append(resp.rawLines, line)
	}

	return nil
}

// expect checks the response from the server is expected or not.
// if the response is not expected, it returns error.
//
// The response is expected if the response is equal to the line.
func (resp *response) expect(line []byte) error {
	if resp.endIndicator == endIndicatorNoReply {
		return nil
	}
	if len(resp.rawLines) != 1 {
		return errors.Wrap(ErrMalformedResponse, "expect only 1 line, but got "+strconv.Itoa(len(resp.rawLines)))
	}

	if bytes.Equal(resp.rawLines[0], line) {
		return nil
	}

	message := "unexpected response: "
	if len(resp.rawLines[0]) <= 256 {
		return errors.New(message + string(resp.rawLines[0]))
	}
	return errors.New(message + string(resp.rawLines[0]))
}

func buildNoReplyResponse() *response {
	return &response{
		endIndicator: endIndicatorNoReply,
		limitedLines: 0,
		specEndLine:  nil,
		rawLines:     nil,
	}
}

func buildLimitedLineResponse(lines uint8) *response {
	return &response{
		endIndicator: endIndicatorLimitedLines,
		limitedLines: lines,
		specEndLine:  nil,
		rawLines:     make([][]byte, 0, lines),
	}
}

func buildSpecEndLineResponse(endLine []byte, predictLines int) *response {
	if predictLines <= 0 {
		predictLines = 8
	}

	return &response{
		endIndicator: endIndicatorSpecificEndLine,
		limitedLines: 0,
		specEndLine:  endLine,
		rawLines:     make([][]byte, 0, predictLines),
	}
}

func base64Encode(src []byte) []byte {
	dst := make([]byte, base64.StdEncoding.EncodedLen(len(src)))
	base64.StdEncoding.Encode(dst, src)
	return dst
}

func base64Decode(src []byte) ([]byte, error) {
	dst := make([]byte, base64.StdEncoding.DecodedLen(len(src)))
	n, err := base64.StdEncoding.Decode(dst, src)
	if err != nil {
		return nil, err
	}

	return dst[:n], nil
}
