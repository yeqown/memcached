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

func (req *request) send(rr conn) (err error) {
	_, err = rr.Write(req.raw)
	return err
}

type responseEndIndicator uint8

const (
	endIndicatorLimitedLines responseEndIndicator = iota
	endIndicatorSpecificEndLine
)

// response represents a structural response from memcached server.
type response struct {
	endIndicator responseEndIndicator

	// limitedLines is the number of limitedLines in response, it helps to read
	// from the connection.
	// If limitedLines equals 0, it means the response is not ready to be read
	// from the connection. 1 means the response is
	// ready to be read from the connection.
	limitedLines uint8
	// specEndLine is the specific end line of the response, it helps to read
	// from the connection.
	specEndLine []byte

	raw []byte

	// parsed response
	err error
}

func (resp *response) recv(rr conn) error {
	switch resp.endIndicator {
	case endIndicatorLimitedLines:
		return resp.read1(rr)
	case endIndicatorSpecificEndLine:
		return resp.read2(rr)
	}

	return ErrUnknownIndicator
}

// read1 reads the response from the connection with limited lines.
func (resp *response) read1(rr conn) error {
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
func (resp *response) read2(rr conn) error {
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

// TODO(@yeqown): reuse response and request objects
func buildResponse1(lines uint8) *response {
	return &response{
		endIndicator: endIndicatorLimitedLines,
		limitedLines: lines,
		specEndLine:  nil,
		raw:          nil,
		err:          nil,
	}
}

func buildResponse2(endLine []byte) *response {
	return &response{
		endIndicator: endIndicatorSpecificEndLine,
		limitedLines: 0,
		specEndLine:  endLine,
		raw:          nil,
		err:          nil,
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
func buildStorageCommand(command, key string, value []byte, flags, expTime uint32, noReply bool) *request {
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

	return &request{
		cmd: []byte(command),
		key: []byte(key),
		raw: raw,
	}
}

// touch <key> <exptime> [noreply]\r\n
func buildTouchCommand(key string, expTime uint32, noReply bool) *request {
	b := newProtocolBuilder().
		AddString("touch").
		AddString(key).
		AddUint(uint64(expTime))

	if noReply {
		b.AddBytes(_NoReplyBytes)
	}

	raw := b.AddCRLF().
		build()

	return &request{
		cmd: []byte("touch"),
		key: []byte(key),
		raw: raw,
	}
}

// cas <key> <flags> <exptime> <bytes> <cas unique> [noreply]\r\n
func buildCasCommand(key string, value []byte, flags, expTime, casUnique uint32, noReply bool) *request {
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

	return &request{
		cmd: []byte("cas"),
		key: []byte(key),
		raw: raw,
	}
}

// buildGetCommand constructs get command.
// get <key>\r\n
func buildGetCommand(key string) *request {
	raw := newProtocolBuilder().
		AddString("get").
		AddString(key).
		AddCRLF().
		build()

	return &request{
		cmd: []byte("get"),
		key: []byte(key),
		raw: raw,
	}
}

// buildGetsCommand constructs gets command.
// gets <key>*\r\n
func buildGetsCommand(keys ...string) *request {
	raw := newProtocolBuilder().
		AddString("gets")

	for _, key := range keys {
		raw.AddString(key)
	}

	return &request{
		cmd: []byte("gets"),
		key: nil,
		raw: raw.AddCRLF().build(),
	}
}

// parseItems parses the response from memcached server.
// VALUE <key> <flags> <bytes> <cas unique>\r\n
// <data block>\r\n
// VALUE <key> <flags> <bytes> <cas unique>\r\n
// <data block>\r\n
// ...
// END\r\n
func parseItems(raw []byte) ([]*Item, error) {
	var items []*Item
	lines := bytes.Split(raw, _CRLFBytes)

	for i := 0; i < len(lines)-1; i++ {
		line := lines[i]
		if bytes.HasPrefix(line, _ValueBytes) {
			parts := bytes.Split(line, _SpaceBytes)
			if len(parts) < 4 {
				return nil, errors.Wrap(ErrMalformedResponse, "invalid VALUE line")
			}

			key := string(parts[1])
			bytesFlags := parts[2]
			bytesLen := parts[3]

			flags, err := strconv.Atoi(string(bytesFlags))
			if err != nil {
				return nil, errors.Wrap(ErrMalformedResponse, "invalid flags")
			}

			// Convert bytesLen to integer
			length, err := strconv.Atoi(string(bytesLen))
			if err != nil {
				return nil, errors.Wrap(ErrMalformedResponse, "invalid bytes length")
			}

			// Read the data block
			i++
			if i >= len(lines)-1 {
				return nil, errors.Wrap(ErrMalformedResponse, "missing data block")
			}
			data := lines[i]

			if len(data) != length {
				return nil, errors.Wrap(ErrMalformedResponse, "data block length mismatch")
			}

			item := &Item{
				Key:   key,
				Value: cleanLine(data),
				Flags: uint32(flags),
			}
			items = append(items, item)
		}
	}

	return items, nil
}
