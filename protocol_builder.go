package memcached

import (
	"bytes"
	"context"
	"encoding/base64"
	"math"
	"strconv"
	"sync"
	"time"
	"unsafe"

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
	_CRLF          = "\r\n"
	_NoReplyBytes  = []byte("noreply")
	_QuitCRLFBytes = []byte("quit\r\n")

	_OKCRLFBytes      = []byte("OK\r\n")
	_ValueBytes       = []byte("VALUE")
	_EndCRLFBytes     = []byte("END\r\n")
	_StoredCRLFBytes  = []byte("STORED\r\n")
	_DeletedCRLFBytes = []byte("DELETED\r\n")
	_TouchedCRLFBytes = []byte("TOUCHED\r\n")
	_VersionBytes     = []byte("VERSION")

	_MetaMNCRLFBytes = []byte("MN\r\n")
)

// forecastCommonFaultLine forecasts the error line from the response line.
// ERROR\r\n
// CLIENT_ERROR <message>\r\n
// SERVER_ERROR <message>\r\n
// NOT_FOUND\r\n
// EXISTS\r\n
// NOT_STORED\r\n
// EN <flags>*\r\n or EN\r\n
// EX <flags>*\r\n or EX\r\n
// NS <flags>*\r\n or NS\r\n
// NF <flags>*\r\n or NF\r\n
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
	//	meta ERROR lines
	case bytes.HasPrefix(line, []byte("NF")):
		return ErrNotFound
	case bytes.HasPrefix(line, []byte("NS")):
		return ErrNotStored
	case bytes.HasPrefix(line, []byte("EX")):
		return ErrExists
	case bytes.HasPrefix(line, []byte("EN")):
		return ErrNotFound

	}

	return nil
}

const (
	// defaultBufferSize is the default size of the buffer.
	// TODO: It is used to avoid the buffer growth, but is 64B the most common case?
	defaultBufferSize = 64
)

var (
	bufferPool = sync.Pool{
		New: func() any {
			return bytes.NewBuffer(make([]byte, 0, defaultBufferSize))
		},
	}
	builderPool = sync.Pool{
		New: func() any {
			return &protocolBuilder{
				buf: bufferPool.Get().(*bytes.Buffer),
			}
		},
	}
)

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
	buf *bytes.Buffer
}

func newProtocolBuilder() *protocolBuilder {
	pb := builderPool.Get().(*protocolBuilder)
	if pb.buf == nil {
		pb.buf = bufferPool.Get().(*bytes.Buffer)
	} else {
		pb.buf.Reset()
	}

	return pb
}

func (b *protocolBuilder) release() {
	if b.buf != nil {
		b.buf.Reset()
		bufferPool.Put(b.buf)
		b.buf = nil
	}

	builderPool.Put(b)
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
	data := b.buf.Bytes()

	result := make([]byte, len(data))
	copy(result, data)

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

var requestPool = sync.Pool{
	New: func() any {
		return &request{
			cmd:        nil,
			key:        nil,
			raw:        nil,
			udpEnabled: false,
		}
	},
}

type request struct {
	cmd []byte // command name
	key []byte // key is nil if the command DOES NOT need key
	raw []byte

	// this field is used to indicate whether the request is UDP enabled.
	// And it's set by the memcached client before sending the request.
	udpEnabled bool
}

func buildRequest(cmd []byte, key []byte, raw []byte) *request {
	req := requestPool.Get().(*request)
	req.cmd = cmd
	req.key = key
	req.raw = raw
	return req
}

func (req *request) release() {
	req.cmd = nil
	req.key = nil
	req.raw = nil
	req.udpEnabled = false

	requestPool.Put(req)
}

func (req *request) send(ctx context.Context, rr memcachedConn, writeTimeout time.Duration) (err error) {
	if has := selectProximateDeadline(ctx, rr, writeTimeout, nowFunc, false); has {
		defer func() { _ = rr.setWriteDeadline(zeroTime) }()
	}

	if req.udpEnabled {
		return req.sendUDP(rr)
	}

	_, err = rr.Write(req.raw)
	return err
}

// udpHeader is the header of UDP datagram, now it's simply set to fixed value.
var udpHeader = []byte{
	0x00,
	0x01, // Request ID
	0x00,
	0x00, // Sequence Number
	0x00,
	0x01, // Total Number of datagrams in this message
	0x00,
	0x00, // Reserved for future use: must be set to 0
}

// sendUDP sends the request to the memcached server.
// https://github.com/memcached/memcached/blob/master/doc/protocol.txt#L1875-L1914
func (req *request) sendUDP(rr memcachedConn) (err error) {
	// 0-1 Request ID
	// 2-3 Sequence Number
	// 4-5 Total Number of datagrams in this message
	// 6-7 Reserved for future use: must be set to 0
	datagram := make([]byte, 0, 8+len(req.raw))
	datagram = append(datagram, udpHeader...)
	datagram = append(datagram, req.raw...)

	_, err = rr.Write(datagram)
	return err
}

var responsePool = sync.Pool{
	New: func() any {
		return &response{
			endIndicator: endIndicatorNoReply,
			limitedLines: 0,
			specEndLine:  nil,
			rawLines:     nil,
			udpEnabled:   false,
		}
	},
}

type responseEndIndicator uint8

const (
	endIndicatorUnknown responseEndIndicator = iota
	// endIndicatorNoReply indicates the response is no reply
	// and the client should not wait for the response.
	endIndicatorNoReply
	// endIndicatorLimitedLines indicates the response is limited lines;
	// the client should read line from response with limited lines with delimiter '\n'.
	endIndicatorLimitedLines
	// endIndicatorSpecificEndLine indicates the response is specific end line;
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

	// This field is used to indicate whether the request is UDP enabled.
	// And it's set by the memcached client before sending the request.
	udpEnabled bool
}

func buildNoReplyResponse() *response {
	resp := responsePool.Get().(*response)
	resp.endIndicator = endIndicatorNoReply
	return resp
}

func buildLimitedLineResponse(lines uint8) *response {
	resp := responsePool.Get().(*response)
	resp.endIndicator = endIndicatorLimitedLines
	resp.limitedLines = lines
	resp.rawLines = make([][]byte, 0, lines)
	return resp
}

func buildSpecEndLineResponse(endLine []byte, predictLines int) *response {
	if predictLines <= 0 {
		predictLines = 8
	}

	resp := responsePool.Get().(*response)
	resp.endIndicator = endIndicatorSpecificEndLine
	resp.specEndLine = endLine
	resp.rawLines = make([][]byte, 0, predictLines)
	return resp
}

func (resp *response) release() {
	resp.endIndicator = endIndicatorUnknown
	resp.limitedLines = 0
	resp.specEndLine = nil
	resp.rawLines = nil
	resp.udpEnabled = false
	responsePool.Put(resp)
}

func (resp *response) recv(ctx context.Context, rr memcachedConn, readTimeout time.Duration) error {
	if has := selectProximateDeadline(ctx, rr, readTimeout, nowFunc, true); has {
		defer func() { _ = rr.setReadDeadline(zeroTime) }()
	}

	switch resp.endIndicator {
	case endIndicatorNoReply:
		return nil
	case endIndicatorLimitedLines:
		// FIXME(@yeqown): read limited line would block waiting for the response
		//  But there's no more lines to read, since error encountered.
		// 	.e.g: "EN kfoo\r\n" means the key "foo" does not exist, but the client
		//  maybe want 2 lines, but only 1 line to read.
		return resp.read1(rr)
	case endIndicatorSpecificEndLine:
		return resp.read2(rr)
	default:
	}

	return ErrUnknownIndicator
}

func selectProximateDeadline(
	ctx context.Context, rr memcachedConn, timeout time.Duration, nowFunc nowFuncType, isRead bool) (ok bool) {

	if ctx == nil {
		ctx = context.Background()
	}
	if timeout < 0 {
		timeout = 0
	}

	var (
		deadline time.Time
		has      bool
	)
	if timeout > 0 {
		deadline = nowFunc().Add(timeout)
		has = true
	}

	if ctxDeadline, ok := ctx.Deadline(); ok {
		if !has || ctxDeadline.Before(deadline) {
			deadline = ctxDeadline
			has = true
		}
	}

	if has {
		if isRead {
			_ = rr.setReadDeadline(deadline)
		} else {
			_ = rr.setWriteDeadline(deadline)
		}
	}

	return has
}

func parseUDPHeader(line []byte) []byte {
	if len(line) < 8 {
		return line
	}

	// cut off the first 8 bytes
	return line[8:]
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
			if resp.udpEnabled {
				line = parseUDPHeader(line)
			}

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
	read := 0
	for {
		// FIXME(@yeqown): read line would cost too much capacity.
		line, err := rr.readLine('\n')
		if err != nil {
			return errors.Wrap(err, "dispatchRequest read")
		}

		if read == 0 && resp.udpEnabled {
			line = parseUDPHeader(line)
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
		read++
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
	if n := len(resp.rawLines); n != 1 {
		return errors.Wrapf(ErrMalformedResponse, "expect only 1 line, but got %d", n)
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

func releaseReqAndResp(req *request, resp *response) {
	if req != nil {
		req.release()
	}

	if resp != nil {
		resp.release()
	}
}

func unsafeStringToByteSlice(s string) []byte {
	return unsafe.Slice(unsafe.StringData(s), len(s))
}

func unsafeByteSliceToString(bs []byte) string {
	return unsafe.String(unsafe.SliceData(bs), len(bs))
}
