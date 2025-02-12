package memcached

import (
	"bytes"
	"strconv"

	"github.com/pkg/errors"
)

// TODO(@yeqown): reuse response and request objects in following building functions.

// Item represents a key-value pair to be got or stored.
type Item struct {
	Key   string
	Value []byte

	// Flags is the flags of the value.
	Flags uint32
	// CAS is a unique value that is used to check-and-set operation.
	// It ONLY returns when you use `Gets` command.
	CAS uint64
}

// MetaItem represents a key-value pair with meta information.
type MetaItem struct {
	Key   []byte
	Value []byte

	/* Meta */
	// CAS is a unique value that is used to check-and-set operation.
	// use MetaGetFlagReturnCAS() or MetaGetFlagReturnCAS() to get this value.
	CAS uint64
	// Flags is the flags of the value.
	// use MetaGetFlagReturnClientFlags() to get this value.
	Flags uint32
	// TTL is the time-to-live of the item. -1 means never expire.
	// use MetaGetFlagReturnTTL() to get this value.
	TTL int64
	// LastAccessedTime is the time since item was last accessed in seconds.
	// use MetaGetFlagReturnLastAccessedTime() to get this value.
	LastAccessedTime int64
	// Size is the size of the stored item on success.
	// use MetaGetFlagReturnSize() or MetaSetFlagReturnSize() to get this value.
	Size uint64
	// Opaque is the opaque value, consumes a token and copies back with response.
	// use MetaGetFlagOpaque(token) to get this value.
	Opaque uint64
	// HitBefore is the flag to return whether item has been hit before as a 0 or 1.
	// use MetaGetFlagReturnHitBefore() to get this value.
	HitBefore bool
}

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
			Key:   string(parts[keyIndex]),
			Value: data,
			Flags: uint32(flags),
			CAS:   cas,
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
	b bool        // b: interpret key as base64 encoded binary value (see metaget)
	c bool        // c: return CAS value if successfully stored.
	C uint64      // C(token): compare CAS value when storing item
	E uint64      // E(token): use token as new CAS value (see meta get for detail)
	F uint32      // F(token): set client flags to token (32 bit unsigned numeric)
	I bool        // I: invalidate. set-to-invalid if supplied CAS is older than item's CAS
	k bool        // k: return key as a token
	O uint64      // O(token): opaque value, consumes a token and copies back with response
	q bool        // q: use noreply semantics for return codes
	s bool        // s: return the size of the stored item on success (ie; new size on append)
	T uint64      // T(token): Time-To-Live for item, see "Expiration" above.
	M metaSetMode // M(token): mode switch to change behavior to: add, replace, append, prepend, set(default)
	N uint64      // N(token): if in append mode, auto vivify on miss with supplied TTL
}

// MetaSetOption is the option to set flags for meta set command.
type MetaSetOption func(*metaSetFlags)

// MetaSetFlagBinaryKey sets the flag to interpret key as binary value.
func MetaSetFlagBinaryKey() MetaSetOption {
	return func(flags *metaSetFlags) { flags.b = true }
}

// MetaSetFlagReturnCAS sets the flag to return CAS value if successfully stored.
func MetaSetFlagReturnCAS() MetaSetOption {
	return func(flags *metaSetFlags) { flags.c = true }
}

// MetaSetFlagCompareCAS sets the flag to compare CAS value when storing item.
func MetaSetFlagCompareCAS(casUnique uint64) MetaSetOption {
	return func(flags *metaSetFlags) { flags.C = casUnique }
}

// MetaSetFlagNewCAS sets the flag to use token as new CAS value.
func MetaSetFlagNewCAS(casUnique uint64) MetaSetOption {
	return func(flags *metaSetFlags) { flags.E = casUnique }
}

// MetaSetFlagClientFlags sets the flag to set client flags to token.
func MetaSetFlagClientFlags(flag uint32) MetaSetOption {
	return func(flags *metaSetFlags) { flags.F = flag }
}

// MetaSetFlagInvalidate sets the flag to set-to-invalid if supplied CAS is older than item's CAS.
func MetaSetFlagInvalidate() MetaSetOption {
	return func(flags *metaSetFlags) { flags.I = true }
}

// MetaSetFlagReturnKey sets the flag to return key as a token.
func MetaSetFlagReturnKey() MetaSetOption {
	return func(flags *metaSetFlags) { flags.k = true }
}

// MetaSetFlagOpaque sets the flag to opaque value, consumes a token and copies back with response.
func MetaSetFlagOpaque(token uint64) MetaSetOption {
	return func(flags *metaSetFlags) { flags.O = token }
}

// MetaSetFlagNoReply sets the flag to use noreply semantics for return codes.
func MetaSetFlagNoReply() MetaSetOption {
	return func(flags *metaSetFlags) { flags.q = true }
}

// MetaSetFlagReturnSize sets the flag to return the size of the stored item on success.
func MetaSetFlagReturnSize() MetaSetOption {
	return func(flags *metaSetFlags) { flags.s = true }
}

// MetaSetFlagTTL sets the flag to Time-To-Live(seconds) for item.
// If the value is 0, the item will never expire.
func MetaSetFlagTTL(ttl uint64) MetaSetOption {
	return func(flags *metaSetFlags) { flags.T = ttl }
}

type metaSetMode string

const (
	// MetaSetModeAdd means add the item only if it does not already exist.
	MetaSetModeAdd metaSetMode = "add"
	// MetaSetModeReplace means replace the item only if it already exists.
	MetaSetModeReplace metaSetMode = "replace"
	// MetaSetModeAppend means append the data to the existing item.
	MetaSetModeAppend metaSetMode = "append"
	// MetaSetModePrepend means prepend the data to the existing item.
	MetaSetModePrepend metaSetMode = "prepend"
	// MetaSetModeSet means set the item regardless of whether it already exists.
	MetaSetModeSet metaSetMode = "set"
)

// MetaSetFlagModeSwitch sets the flag to mode switch to change behavior to: add, replace, append, prepend, set(default).
func MetaSetFlagModeSwitch(mode metaSetMode) MetaSetOption {
	return func(flags *metaSetFlags) { flags.M = mode }
}

// MetaSetFlagAutoVivify sets the flag to auto vivify on miss with supplied TTL.
func MetaSetFlagAutoVivify(ttl uint64) MetaSetOption {
	return func(flags *metaSetFlags) { flags.N = ttl }
}

// ms <key> <datalen> <flags>*\r\n
// <data block>\r\n
func buildMetaSetCommand(key, value []byte, flags *metaSetFlags) (*request, *response) {
	if flags.b {
		key = base64Encode(key)
	}

	b := newProtocolBuilder().
		AddString("ms").
		AddBytes(key).
		AddInt(len(value))

	b.AddFlagBool("b", flags.b)
	b.AddFlagBool("c", flags.c)
	b.AddFlagUint("C", flags.C)
	b.AddFlagUint("E", flags.E)
	b.AddFlagUint("F", uint64(flags.F))
	b.AddFlagBool("I", flags.I)
	b.AddFlagBool("k", flags.k)
	b.AddFlagUint("O", flags.O)
	b.AddFlagBool("q", flags.q)
	b.AddFlagBool("s", flags.s)
	b.AddFlagUint("T", flags.T)
	b.AddFlagString("M", string(flags.M))
	b.AddFlagUint("N", flags.N)

	raw := b.AddCRLF().
		AddBytes(value).
		AddCRLF().
		build()

	req := &request{
		cmd: []byte("ms"),
		key: []byte(key),
		raw: raw,
	}

	var resp *response
	if flags.q {
		resp = buildNoReplyResponse()
	} else {
		resp = buildLimitedLineResponse(1)
	}

	return req, resp
}

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

// MetaGetFlagBinaryKey sets the flag to interpret key as base64 encoded binary value.
func MetaGetFlagBinaryKey() MetaGetOption {
	return func(flags *metaGetFlags) { flags.b = true }
}

// MetaGetFlagReturnCAS sets the flag to return item cas token.
func MetaGetFlagReturnCAS() MetaGetOption {
	return func(flags *metaGetFlags) { flags.c = true }
}

// MetaGetFlagReturnClientFlags sets the flag to return client flags token.
func MetaGetFlagReturnClientFlags() MetaGetOption {
	return func(flags *metaGetFlags) { flags.f = true }
}

// MetaGetFlagReturnHitBefore sets the flag to return whether item has been hit before as a 0 or 1.
func MetaGetFlagReturnHitBefore() MetaGetOption {
	return func(flags *metaGetFlags) { flags.h = true }
}

// MetaGetFlagReturnKey sets the flag to return key as a token.
func MetaGetFlagReturnKey() MetaGetOption {
	return func(flags *metaGetFlags) { flags.k = true }
}

// MetaGetFlagReturnLastAccessedTime sets the flag to return time since item was last accessed in seconds.
func MetaGetFlagReturnLastAccessedTime() MetaGetOption {
	return func(flags *metaGetFlags) { flags.l = true }
}

// MetaGetFlagOpaque sets the flag to opaque value, consumes a token and copies back with response.
func MetaGetFlagOpaque(token uint64) MetaGetOption {
	return func(flags *metaGetFlags) { flags.O = token }
}

// MetaGetFlagNoReply sets the flag to use noreply semantics for return codes.
func MetaGetFlagNoReply() MetaGetOption {
	return func(flags *metaGetFlags) { flags.q = true }
}

// MetaGetFlagReturnSize sets the flag to return item size token.
func MetaGetFlagReturnSize() MetaGetOption {
	return func(flags *metaGetFlags) { flags.s = true }
}

// MetaGetFlagReturnTTL sets the flag to return item TTL remaining in seconds (-1 for unlimited).
func MetaGetFlagReturnTTL() MetaGetOption {
	return func(flags *metaGetFlags) { flags.t = true }
}

// MetaGetFlagDontBumpLRU sets the flag to don't bump the item in the LRU.
func MetaGetFlagDontBumpLRU() MetaGetOption {
	return func(flags *metaGetFlags) { flags.u = true }
}

// MetaGetFlagReturnValue sets the flag to return item value in <data block>.
func MetaGetFlagReturnValue() MetaGetOption {
	return func(flags *metaGetFlags) { flags.v = true }
}

// MetaGetFlagNewCAS sets the flag to use token as new CAS value if item is modified.
func MetaGetFlagNewCAS(casUnique uint64) MetaGetOption {
	return func(flags *metaGetFlags) { flags.E = casUnique }
}

// MetaGetFlagVivifyOnMiss sets the flag to vivify on miss, takes TTL as an argument.
func MetaGetFlagVivifyOnMiss(ttl uint64) MetaGetOption {
	return func(flags *metaGetFlags) { flags.N = ttl }
}

// MetaGetFlagWinForRecache sets the flag to if remaining TTL is less than token, win for recache.
func MetaGetFlagWinForRecache(ttl uint64) MetaGetOption {
	return func(flags *metaGetFlags) { flags.R = ttl }
}

// MetaGetFlagUpdateRemainingTTL sets the flag to update remaining TTL.
func MetaGetFlagUpdateRemainingTTL(ttl uint64) MetaGetOption {
	return func(flags *metaGetFlags) { flags.T = ttl }
}

// MetaGetFlagClientHasWonRecache sets the flag to client has "won" the recache flag.
func MetaGetFlagClientHasWonRecache() MetaGetOption {
	return func(flags *metaGetFlags) { flags.W = true }
}

// MetaGetFlagItemIsStale sets the flag to item is stale.
func MetaGetFlagItemIsStale() MetaGetOption {
	return func(flags *metaGetFlags) { flags.X = true }
}

// MetaGetFlagItemHasSentWinningFlag sets the flag to item has already sent a winning flag.
func MetaGetFlagItemHasSentWinningFlag() MetaGetOption {
	return func(flags *metaGetFlags) { flags.Z = true }
}

// mg <key> <flags>*\r\n
func buildMetaGetCommand(key []byte, flags *metaGetFlags) (*request, *response) {
	if flags.b {
		key = base64Encode(key)
	}

	b := newProtocolBuilder().
		AddString("mg").
		AddBytes(key)

	b.AddFlagBool("b", flags.b)
	b.AddFlagBool("c", flags.c)
	b.AddFlagBool("f", flags.f)
	b.AddFlagBool("h", flags.h)
	b.AddFlagBool("k", flags.k)
	b.AddFlagBool("l", flags.l)
	b.AddFlagUint("O", flags.O)
	b.AddFlagBool("q", flags.q)
	b.AddFlagBool("s", flags.s)
	b.AddFlagBool("t", flags.t)
	b.AddFlagBool("u", flags.u)
	b.AddFlagBool("v", flags.v)

	b.AddFlagUint("E", flags.E)
	b.AddFlagUint("N", flags.N)
	b.AddFlagUint("R", flags.R)
	b.AddFlagUint("T", flags.T)

	raw := b.AddCRLF().
		build()

	req := &request{
		cmd: []byte("mg"),
		key: []byte(key),
		raw: raw,
	}

	var resp *response
	if flags.q {
		resp = buildNoReplyResponse()
	} else {
		resp = buildLimitedLineResponse(2)
	}

	return req, resp
}

// parseMetaItem parses the response from memcached server. It coule be:
//
//	<CD> <flags>*\r\n
//
// .e.g:
//
//	HD c26 kZm9v b O456 s3\r\n
//
// CD is one of: HD(succeed), NF(not found), NS(not stored), EX(exists), EN(miss).
//
//	VA(value).
//
// VA is specified as: VA <size> <flags>*\r\n<data block>\r\n.
func parseMetaItem(lines [][]byte, item *MetaItem, noReply bool) error {
	if noReply && len(lines) == 0 {
		return nil
	}

	// figure out the <CD>
	if len(lines) == 0 {
		return errors.Wrap(ErrMalformedResponse, "missing response")
	}

	// Normal CD handling
	parts := bytes.Split(trimCRLF(lines[0]), _SpaceBytes)
	if len(parts) < 1 {
		return errors.Wrap(ErrMalformedResponse, "invalid response")
	}
	const (
		CDIndex      = 0
		dataLenIndex = 1
	)
	cd := parts[CDIndex]
	switch string(cd) {
	case "NF":
		return ErrNotFound
	case "NS":
		return ErrNotStored
	case "EX":
		return ErrExists
	case "EN":
		return ErrNotFound
	}

	if !bytes.Equal(cd, []byte("VA")) {
		parseFlags(parts, 1, item)
		return nil
	}

	// VA handling
	item.Size, _ = strconv.ParseUint(string(parts[dataLenIndex]), 10, 32)
	parseFlags(parts, 2, item)

	if len(lines) < 2 {
		return errors.Wrap(ErrMalformedResponse, "missing value")
	}

	item.Value = trimCRLF(lines[1])
	return nil
}

// CD <flags>*\r\n
// .e.g:
//
//	HD c26 kZm9v b O456 s3\r\n
func parseFlags(parts [][]byte, startPos int, item *MetaItem) {
	parseUint := func(b []byte) uint64 {
		v, _ := strconv.ParseUint(string(b), 10, 64)
		return v
	}

	parseInt := func(b []byte) int64 {
		v, _ := strconv.ParseInt(string(b), 10, 64)
		return v
	}

	for i := startPos; i < len(parts); i++ {
		switch parts[i][0] {
		case 'c':
			item.CAS = parseUint(parts[i][1:])
		case 'f':
			item.Flags = uint32(parseUint(parts[i][1:]))
		case 't':
			item.TTL = int64(parseInt(parts[i][1:]))
		case 'l':
			item.LastAccessedTime = int64(parseUint(parts[i][1:]))
		case 's':
			item.Size = parseUint(parts[i][1:])
		case 'O':
			item.Opaque = parseUint(parts[i][1:])
		case 'h':
			item.HitBefore = parseUint(parts[i][1:]) == 1
			// NO need to parse key again in client.
			//case 'k':
			//	item.Key = string(parts[i][1:])
		}
	}
}
