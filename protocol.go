package memcached

import (
	"bytes"
	"strconv"

	"github.com/pkg/errors"
)

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

	raw []byte
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
		line, err := rr.Read('\n')
		if err != nil {
			return errors.Wrap(err, "doRequest read")
		}

		if read == 0 {
			if err = forecastCommonFaultLine(line); err != nil {
				return err
			}
		}

		resp.raw = append(resp.raw, line...)
		read++
	}

	return nil
}

// read2 reads the response from the connection with specific end line.
func (resp *response) read2(rr memcachedConn) error {
	for {
		line, err := rr.Read('\n')
		if err != nil {
			return errors.Wrap(err, "doRequest read")
		}

		if bytes.Equal(line, resp.specEndLine) {
			break
		}

		if err = forecastCommonFaultLine(line); err != nil {
			return err
		}

		resp.raw = append(resp.raw, line...)
	}

	return nil
}

// expect checks the response from the server is expected or not.
// if the response is not expected, it returns error.
func (resp *response) expect(lines []byte) error {
	if resp.endIndicator == endIndicatorNoReply {
		return nil
	}

	if bytes.Equal(resp.raw, lines) {
		return nil
	}

	message := "unexpected response: "
	if len(resp.raw) <= 256 {
		return errors.New(message + string(resp.raw))
	}
	return errors.New(message + string(resp.raw[:256]))
}

func buildNoReplyResponse() *response {
	return &response{
		endIndicator: endIndicatorNoReply,
		limitedLines: 0,
		specEndLine:  nil,
		raw:          nil,
	}
}

// TODO(@yeqown): reuse response and request objects
func buildLimitedLineResponse(lines uint8) *response {
	return &response{
		endIndicator: endIndicatorLimitedLines,
		limitedLines: lines,
		specEndLine:  nil,
		raw:          nil,
	}
}

func buildSpecEndLineResponse(endLine []byte) *response {
	return &response{
		endIndicator: endIndicatorSpecificEndLine,
		limitedLines: 0,
		specEndLine:  endLine,
		raw:          nil,
	}
}

func buildVersionCommand() *request {
	return &request{
		cmd: []byte("version"),
		key: nil,
		raw: []byte("version\r\n"),
	}
}

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

// buildStorageCommand constructs storage class command, including:
//
//	set/add/replace/append/prepend, etc.
//
// <command name> <key> <flags> <exptime> <bytes> [noreply]\r\n
// <data block>\r\n
func buildStorageCommand(command, key string, value []byte, flags, expTime uint32, noReply bool) (*request, *response) {
	b := newProtocolBuilder().
		AddString(command).
		AddString(key).           // key
		AddUint(uint64(flags)).   // flags
		AddUint(uint64(expTime)). // exptime
		AddInt(len(value))        // bytes

	if noReply {
		b.AddBytes(_NoReplyBytes)
	}

	raw := b.AddCRLF().
		AddBytes(value). // data block
		AddCRLF().
		build()

	req := &request{
		cmd: []byte(command),
		key: []byte(key),
		raw: raw,
	}

	var resp *response
	if noReply {
		resp = buildNoReplyResponse()
	} else {
		resp = buildLimitedLineResponse(1)
	}

	return req, resp
}

// delete <key> [noreply]\r\n
func buildDeleteCommand(key string, noReply bool) (*request, *response) {
	b := newProtocolBuilder().
		AddString("delete").
		AddString(key)

	if noReply {
		b.AddBytes(_NoReplyBytes)
	}

	req := &request{
		cmd: []byte("delete"),
		key: []byte(key),
		raw: b.AddCRLF().
			build(),
	}

	var resp *response
	if noReply {
		resp = buildNoReplyResponse()
	} else {
		resp = buildLimitedLineResponse(1)
	}

	return req, resp
}

// touch <key> <exptime> [noreply]\r\n
func buildTouchCommand(key string, expTime uint32, noReply bool) (*request, *response) {
	b := newProtocolBuilder().
		AddString("touch").
		AddString(key).
		AddUint(uint64(expTime))

	if noReply {
		b.AddBytes(_NoReplyBytes)
	}

	raw := b.AddCRLF().
		build()

	req := &request{
		cmd: []byte("touch"),
		key: []byte(key),
		raw: raw,
	}

	var resp *response
	if noReply {
		resp = buildNoReplyResponse()
	} else {
		resp = buildLimitedLineResponse(1)
	}

	return req, resp
}

// cas <key> <flags> <exptime> <bytes> <cas unique> [noreply]\r\n
func buildCasCommand(
	key string, value []byte, flags, expTime uint32, casUnique uint64, noReply bool) (*request, *response) {
	b := newProtocolBuilder().
		AddString("cas").          // command
		AddString(key).            // key
		AddUint(uint64(flags)).    // flags
		AddUint(uint64(expTime)).  // exptime
		AddInt(len(value)).        // bytes
		AddUint(uint64(casUnique)) // cas unique

	if noReply {
		b.AddBytes(_NoReplyBytes)
	}

	raw := b.AddCRLF().
		AddBytes(value). // data block
		AddCRLF().
		build()

	req := &request{
		cmd: []byte("cas"),
		key: []byte(key),
		raw: raw,
	}

	var resp *response
	if noReply {
		resp = buildNoReplyResponse()
	} else {
		resp = buildLimitedLineResponse(1)
	}

	return req, resp
}

// buildGetCommand constructs get command.
// get <key>\r\n
func buildGetCommand(key string) (*request, *response) {
	raw := newProtocolBuilder().
		AddString("get").
		AddString(key).
		AddCRLF().
		build()

	req := &request{
		cmd: []byte("get"),
		key: []byte(key),
		raw: raw,
	}

	resp := buildLimitedLineResponse(3)

	return req, resp
}

// buildGetsCommand constructs gets command.
// gets <key>*\r\n
func buildGetsCommand(keys ...string) (*request, *response) {
	raw := newProtocolBuilder().
		AddString("gets")

	for _, key := range keys {
		raw.AddString(key)
	}

	req := &request{
		cmd: []byte("gets"),
		key: nil,
		raw: raw.AddCRLF().build(),
	}

	resp := buildSpecEndLineResponse(_EndCRLFBytes)

	return req, resp
}

// parseValueItems parses the response from memcached server.
// VALUE <key> <flags> <bytes> <cas unique>\r\n
// <data block>\r\n
// VALUE <key> <flags> <bytes> <cas unique>\r\n
// <data block>\r\n
// ...
// END\r\n
func parseValueItems(raw []byte) ([]*Item, error) {
	var items []*Item
	lines := bytes.Split(raw, _CRLFBytes)

	var (
		flags, _bytes uint64
		casUniq       uint64
		err           error
	)

	for i := 0; i < len(lines)-1; i++ {
		line := lines[i]
		flags, _bytes, casUniq = 0, 0, 0

		if bytes.HasPrefix(line, _ValueBytes) {
			parts := bytes.Split(line, _SpaceBytes)
			if len(parts) < 4 {
				return nil, errors.Wrap(ErrMalformedResponse, "invalid VALUE line")
			}

			key := string(parts[1])
			bytesFlags := parts[2]
			bytesLen := parts[3]
			if len(parts) == 5 {
				casUniqBytes := parts[4]
				casUniq, err = strconv.ParseUint(string(casUniqBytes), 10, 64)
				if err != nil {
					return nil, errors.Wrap(ErrMalformedResponse, "invalid cas unique")
				}
			}

			flags, err = strconv.ParseUint(string(bytesFlags), 10, 32)
			if err != nil {
				return nil, errors.Wrap(ErrMalformedResponse, "invalid flags")
			}

			// Convert bytesLen to integer
			_bytes, err = strconv.ParseUint(string(bytesLen), 10, 64)
			if err != nil {
				return nil, errors.Wrap(ErrMalformedResponse, "invalid bytes length")
			}

			// Read the data block
			i++
			if i >= len(lines)-1 {
				return nil, errors.Wrap(ErrMalformedResponse, "missing data block")
			}
			data := lines[i]

			if uint64(len(data)) != _bytes {
				return nil, errors.Wrap(ErrMalformedResponse, "data block length mismatch")
			}

			item := &Item{
				Key:       key,
				Value:     trimCRLF(data),
				Flags:     uint32(flags),
				CASUnique: casUniq,
			}
			items = append(items, item)
		}
	}

	return items, nil
}
