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

// parseValueItems parses the response from memcached server, the response
// is a list of items, each item is a key-value pair.
//
// For example:
// VALUE <key> <flags> <bytes> <cas unique>\r\n
// <data block>\r\n
// VALUE <key> <flags> <bytes> <cas unique>\r\n
// <data block>\r\n
// ...
// END\r\n
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
		rn             = n
		flags, dataLen uint64
		cas            uint64
		err            error
	)

	if !withoutEndLine {
		// avoid iterate the last line "END\r\n"
		rn = n - 1
	}

	const (
		keyIndex     = 1
		flagsIndex   = 2
		dataLenIndex = 3
		casIndex     = 4

		withCasLen = 5
	)

	for i := 0; i < rn; i += 2 {
		line := trimCRLF(lines[i])
		cas = 0

		if !bytes.HasPrefix(line, _ValueBytes) {
			continue
		}

		parts := bytes.Split(line, _SpaceBytes)
		if len(parts) < 4 {
			return nil, errors.Wrap(ErrMalformedResponse, "invalid VALUE line")
		}

		flagsBytes := parts[flagsIndex]
		dataLenBytes := parts[dataLenIndex]
		// Parse flags and data length
		flags, err = strconv.ParseUint(string(flagsBytes), 10, 32)
		if err != nil {
			return nil, errors.Wrap(ErrMalformedResponse, "invalid flags")
		}
		dataLen, err = strconv.ParseUint(string(dataLenBytes), 10, 64)
		if err != nil {
			return nil, errors.Wrap(ErrMalformedResponse, "invalid bytes length")
		}
		// Parse cas unique if exists
		if len(parts) == withCasLen {
			casBytes := parts[casIndex]
			cas, err = strconv.ParseUint(string(casBytes), 10, 64)
			if err != nil {
				return nil, errors.Wrap(ErrMalformedResponse, "invalid cas unique")
			}
		}

		// Read the data block
		if i+1 >= n {
			return nil, errors.Wrap(ErrMalformedResponse, "missing data block")
		}
		data := trimCRLF(lines[i+1])
		if len(data) != int(dataLen) {
			return nil, errors.Wrap(ErrMalformedResponse, "data block length mismatch")
		}

		item := &Item{
			Key:       string(parts[keyIndex]),
			Value:     data,
			Flags:     uint32(flags),
			CASUnique: cas,
		}
		items = append(items, item)
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

type metaSetFlags struct {
	b bool   // b: interpret key as base64 encoded binary value (see metaget)
	c bool   // c: return CAS value if successfully stored.
	C uint64 // C(token): compare CAS value when storing item
	E uint64 // E(token): use token as new CAS value (see meta get for detail)
	F uint32 // F(token): set client flags to token (32 bit unsigned numeric)
	I bool   // I: invalidate. set-to-invalid if supplied CAS is older than item's CAS
	k bool   // k: return key as a token
	O uint64 // O(token): opaque value, consumes a token and copies back with response
	q bool   // q: use noreply semantics for return codes
	s bool   // s: return the size of the stored item on success (ie; new size on append)
	T uint64 // T(token): Time-To-Live for item, see "Expiration" above.
	M uint64 // M(token): mode switch to change behavior to add, replace, append, prepend
	N uint64 // N(token): if in append mode, auto vivify on miss with supplied TTL
}

// MetaSetOption is the option to set flags for meta set command.
type MetaSetOption func(*metaSetFlags)

// ms <key> <datalen> <flags>*\r\n
// <data block>\r\n
func buildMetaSetCommand(key string, value []byte, flags *metaSetFlags) (*request, *response) {
	b := newProtocolBuilder().
		AddString("ms").
		AddString(key).
		AddInt(len(value))

	// TODO(@yeqown): add flags into command
	if flags != nil {
		_ = flags
	}

	raw := b.AddCRLF().
		AddBytes(value).
		AddCRLF().
		build()

	req := &request{
		cmd: []byte("ms"),
		key: []byte(key),
		raw: raw,
	}

	resp := buildLimitedLineResponse(1)

	return req, resp
}

// These flags can modify the item:
// - E(token): use token as new CAS value if item is modified
// - N(token): vivify on miss, takes TTL as a argument
// - R(token): if remaining TTL is less than token, win for recache
// - T(token): update remaining TTL
//
// These extra flags can be added to the response:
// - W: client has "won" the recache flag
// - X: item is stale
// - Z: item has already sent a winning flag
type metaGetFlags struct {
	b bool   // b: interpret key as base64 encoded binary value
	c bool   // c: return item cas token
	f bool   // f: return client flags token
	h bool   // h: return whether item has been hit before as a 0 or 1
	k bool   // k: return key as a token
	l bool   // l: return time since item was last accessed in seconds
	O uint64 // O(token): opaque value, consumes a token and copies back with response
	q bool   // q: use noreply semantics for return codes.
	s bool   // s: return item size token
	t bool   // t: return item TTL remaining in seconds (-1 for unlimited)
	u bool   // u: don't bump the item in the LRU
	v bool   // v: return item value in <data block>

	// These flags can modify the item:
	E uint64 // E(token): use token as new CAS value if item is modified
	N uint64 // N(token): vivify on miss, takes TTL as a argument
	R uint64 // R(token): if remaining TTL is less than token, win for recache
	T uint64 // T(token): update remaining TTL

	// These extra flags can be added to the response:
	W bool // W: client has "won" the recache flag
	X bool // X: item is stale
	Z bool // Z: item has already sent a winning flag
}

// MetaGetOption is the option to set flags for meta get command.
type MetaGetOption func(*metaGetFlags)

// mg <key> <flags>*\r\n
func buildMetaGetCommand(key string, flags *metaGetFlags) (*request, *response) {
	b := newProtocolBuilder().
		AddString("mg").
		AddString(key)

	// TODO(@yeqown): add flags into command
	if flags != nil {
		_ = flags
	}

	raw := b.AddCRLF().
		build()

	req := &request{
		cmd: []byte("mg"),
		key: []byte(key),
		raw: raw,
	}

	resp := buildSpecEndLineResponse(_MetaEndCRLFBytes, 1)

	return req, resp
}
