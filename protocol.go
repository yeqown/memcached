package memcached

import (
	"bytes"
	"strconv"

	"github.com/pkg/errors"
)

// TODO(@yeqown): reuse response and request objects in following building functions.

func buildAuthCommand(username, password string) (*request, *response) {
	raw := newProtocolBuilder().
		AddString("auth").
		AddString(username).
		AddString(password).
		AddCRLF().
		build()

	req := &request{
		cmd: []byte("auth"),
		key: nil,
		raw: raw,
	}

	resp := buildLimitedLineResponse(1)

	return req, resp
}

func buildVersionCommand() *request {
	return &request{
		cmd: []byte("version"),
		key: nil,
		raw: []byte("version\r\n"),
	}
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

// buildGetsCommand constructs gets command.
// get/gets <key>*\r\n
func buildGetsCommand(command string, keys ...string) (*request, *response) {
	b := newProtocolBuilder().
		AddString(command)

	for _, key := range keys {
		b.AddString(key)
	}
	b.AddCRLF()

	req := &request{
		cmd: []byte(command),
		key: nil,
		raw: b.build(),
	}

	resp := buildSpecEndLineResponse(_EndCRLFBytes, len(keys)*2+1)

	return req, resp
}

// buildGetAndTouchCommand constructs get and touch command.
// gat/gats <key> <exptime>\r\n
func buildGetAndTouchesCommand(command string, expiry uint32, keys ...string) (*request, *response) {
	b := newProtocolBuilder().
		AddString(command)

	for _, key := range keys {
		b.AddString(key)
	}

	b.AddUint(uint64(expiry)).
		AddCRLF()

	req := &request{
		cmd: []byte(command),
		key: []byte(keys[0]),
		raw: b.build(),
	}

	resp := buildSpecEndLineResponse(_EndCRLFBytes, len(keys)*2+1)

	return req, resp
}

// parseValueItems parses the response from memcached server.
// VALUE <key> <flags> <bytes> <cas unique>\r\n
// <data block>\r\n
// VALUE <key> <flags> <bytes> <cas unique>\r\n
// <data block>\r\n
// ...
//
// WITHOUT "END\r\n"
func parseValueItems(lines [][]byte, withoutEndLine bool) ([]*Item, error) {
	var items []*Item

	n := len(lines)
	if withoutEndLine && n%2 != 0 {
		// n must be even
		return nil, errors.Wrap(ErrMalformedResponse, "want times of 2 lines, got "+strconv.Itoa(n))
	}
	if !withoutEndLine && n%2 == 0 {
		// n must be odd
		return nil, errors.Wrap(ErrMalformedResponse, "want odd lines, got "+strconv.Itoa(n))
	}

	var (
		rn            = n
		flags, _bytes uint64
		casUniq       uint64
		err           error
	)

	if !withoutEndLine {
		// avoid iterate the last line "END\r\n"
		rn = n - 1
	}

	for i := 0; i < rn; i++ {
		line := trimCRLF(lines[i])
		casUniq = 0

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
			if i >= len(lines) {
				return nil, errors.Wrap(ErrMalformedResponse, "missing data block")
			}
			data := trimCRLF(lines[i])

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

// incr/decr <key> <value> [noreply]\r\n
func buildArithmeticCommand(command, key string, delta uint64, noReply bool) (*request, *response) {
	b := newProtocolBuilder().
		AddString(command).
		AddString(key).
		AddUint(delta)

	if noReply {
		b.AddBytes(_NoReplyBytes)
	}

	raw := b.AddCRLF().build()

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

// parseArithmeticResponse handles the response with incr/decr command.
// <value>\r\n
func parseArithmetic(line []byte) (uint64, error) {
	if len(line) == 0 {
		// noReply mode enabled
		return 0, nil
	}

	return strconv.ParseUint(string(trimCRLF(line)), 10, 64)
}
