package memcached

import (
	"bytes"
	"strconv"

	"github.com/pkg/errors"
)

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
	} else if flags.v {
		resp = buildLimitedLineResponse(2)
	} else {
		resp = buildLimitedLineResponse(1)
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
			// case 'k':
			//	item.Key = string(parts[i][1:])
		}
	}
}

// MetaDeleteOption is used to set options for MetaDelete command.
type MetaDeleteOption func(*metaDeleteFlags)

type metaDeleteFlags struct {
	b bool   // b: interpret key as base64 encoded binary value
	C uint64 // C(token): compare CAS value
	E uint64 // E(token): use token as new CAS value
	I bool   // I: invalidate. mark as stale, bumps CAS.
	k bool   // k: return key
	O uint64 // O(token): opaque to copy back.
	q bool   // q: noreply
	T uint64 // T(token): updates TTL, only when paired with the 'I' flag
	x bool   // x: removes the item value, but leaves the item.
}

// MetaDeleteFlagBinaryKey sets the flag to interpret key as base64 encoded binary value.
func MetaDeleteFlagBinaryKey() MetaDeleteOption {
	return func(flags *metaDeleteFlags) { flags.b = true }
}

// MetaDeleteFlagCompareCAS sets the flag to compare CAS value.
func MetaDeleteFlagCompareCAS(casUnique uint64) MetaDeleteOption {
	return func(flags *metaDeleteFlags) { flags.C = casUnique }
}

// MetaDeleteFlagNewCAS sets the flag to use token as new CAS value.
func MetaDeleteFlagNewCAS(casUnique uint64) MetaDeleteOption {
	return func(flags *metaDeleteFlags) { flags.E = casUnique }
}

// MetaDeleteFlagInvalidate sets the flag to invalidate. mark as stale, bumps CAS.
func MetaDeleteFlagInvalidate() MetaDeleteOption {
	return func(flags *metaDeleteFlags) { flags.I = true }
}

// MetaDeleteFlagReturnKey sets the flag to return key.
func MetaDeleteFlagReturnKey() MetaDeleteOption {
	return func(flags *metaDeleteFlags) { flags.k = true }
}

// MetaDeleteFlagOpaque sets the flag to opaque to copy back.
func MetaDeleteFlagOpaque(token uint64) MetaDeleteOption {
	return func(flags *metaDeleteFlags) { flags.O = token }
}

// MetaDeleteFlagNoReply sets the flag to noreply.
func MetaDeleteFlagNoReply() MetaDeleteOption {
	return func(flags *metaDeleteFlags) { flags.q = true }
}

// MetaDeleteFlagUpdateTTL sets the flag to updates TTL,
// only when paired with the 'I' flag (MetaDeleteFlagInvalidate).
func MetaDeleteFlagUpdateTTL(ttl uint64) MetaDeleteOption {
	return func(flags *metaDeleteFlags) { flags.T = ttl }
}

// MetaDeleteFlagRemoveValueOnly sets the flag to
// removes the item value, but leaves the item.
func MetaDeleteFlagRemoveValueOnly() MetaDeleteOption {
	return func(flags *metaDeleteFlags) { flags.x = true }
}

// md <key> <flags>*\r\n
func buildMetaDeleteCommand(key []byte, flags *metaDeleteFlags) (*request, *response) {
	if flags.b {
		key = base64Encode(key)
	}

	b := newProtocolBuilder().
		AddString("md").
		AddBytes(key)

	b.AddFlagBool("b", flags.b)
	b.AddFlagUint("C", flags.C)
	b.AddFlagUint("E", flags.E)
	b.AddFlagBool("I", flags.I)
	b.AddFlagBool("k", flags.k)
	b.AddFlagUint("O", flags.O)
	b.AddFlagBool("q", flags.q)
	b.AddFlagUint("T", flags.T)
	b.AddFlagBool("x", flags.x)

	raw := b.AddCRLF().
		build()

	req := &request{
		cmd: []byte("md"),
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

// MetaArithmeticOption is used to set options for MetaArithmetic command.
type MetaArithmeticOption func(*metaArithmeticFlags)

type metaArithmeticFlags struct {
	b bool               // b: interpret key as base64 encoded binary value
	C uint64             // C(token): compare CAS value
	E uint64             // E(token): use token as new CAS value
	N uint64             // N(token): auto create item on miss with supplied TTL
	J uint64             // J(token): initial value to use if auto created after miss (default 0)
	D uint64             // D(token): delta to apply (decimal unsigned 64-bit number, default 1)
	T uint64             // T(token): update TTL on success
	M metaArithmeticMode // M(token): mode switch to change between incr and decr modes.
	O uint64             // O(token): opaque value, consumes a token and copies back with response
	q bool               // q: use noreply semantics for return codes
	t bool               // t: return current TTL
	c bool               // c: return current CAS value if successful.
	v bool               // v: return new value
	k bool               // k: return key as a token
}

// MetaArithmeticFlagBinaryKey sets the flag to interpret key as base64 encoded binary value.
func MetaArithmeticFlagBinaryKey() MetaArithmeticOption {
	return func(flags *metaArithmeticFlags) { flags.b = true }
}

// MetaArithmeticFlagCompareCAS sets the flag to compare CAS value.
func MetaArithmeticFlagCompareCAS(casUnique uint64) MetaArithmeticOption {
	return func(flags *metaArithmeticFlags) { flags.C = casUnique }
}

// MetaArithmeticFlagNewCAS sets the flag to use token as new CAS value.
func MetaArithmeticFlagNewCAS(casUnique uint64) MetaArithmeticOption {
	return func(flags *metaArithmeticFlags) { flags.E = casUnique }
}

// MetaArithmeticFlagAutoCreate sets the flag to auto create item on miss with supplied TTL.
func MetaArithmeticFlagAutoCreate(ttl uint64) MetaArithmeticOption {
	return func(flags *metaArithmeticFlags) { flags.N = ttl }
}

// MetaArithmeticFlagInitialValue sets the flag to initial value to use if auto created after miss (default 0).
func MetaArithmeticFlagInitialValue(v uint64) MetaArithmeticOption {
	return func(flags *metaArithmeticFlags) { flags.J = v }
}

// MetaArithmeticFlagUpdateTTL sets the flag to update TTL on success.
func MetaArithmeticFlagUpdateTTL(ttl uint64) MetaArithmeticOption {
	return func(flags *metaArithmeticFlags) { flags.T = ttl }
}

type metaArithmeticMode string

const (
	// MetaArithmeticModeIncr means increment the item. also alias for "+".
	MetaArithmeticModeIncr metaArithmeticMode = "I"
	// MetaArithmeticModeDecr means decrement the item. also alias for "-".
	MetaArithmeticModeDecr metaArithmeticMode = "D"
)

// MetaArithmeticFlagModeSwitch sets the flag to mode switch to change between incr and decr modes.
func MetaArithmeticFlagModeSwitch(mode metaArithmeticMode) MetaArithmeticOption {
	return func(flags *metaArithmeticFlags) { flags.M = mode }
}

// MetaArithmeticFlagOpaque sets the flag to opaque value, consumes a token and copies back with response.
func MetaArithmeticFlagOpaque(token uint64) MetaArithmeticOption {
	return func(flags *metaArithmeticFlags) { flags.O = token }
}

// MetaArithmeticFlagNoReply sets the flag to use noreply semantics for return codes.
func MetaArithmeticFlagNoReply() MetaArithmeticOption {
	return func(flags *metaArithmeticFlags) { flags.q = true }
}

// MetaArithmeticFlagReturnTTL sets the flag to return current TTL.
func MetaArithmeticFlagReturnTTL() MetaArithmeticOption {
	return func(flags *metaArithmeticFlags) { flags.t = true }
}

// MetaArithmeticFlagReturnCAS sets the flag to return current CAS value if successful.
func MetaArithmeticFlagReturnCAS() MetaArithmeticOption {
	return func(flags *metaArithmeticFlags) { flags.c = true }
}

// MetaArithmeticFlagReturnValue sets the flag to return new value.
func MetaArithmeticFlagReturnValue() MetaArithmeticOption {
	return func(flags *metaArithmeticFlags) { flags.v = true }
}

// MetaArithmeticFlagReturnKey sets the flag to return key as a token.
func MetaArithmeticFlagReturnKey() MetaArithmeticOption {
	return func(flags *metaArithmeticFlags) { flags.k = true }
}

func buildMetaArithmeticCommand(key []byte, delta uint64, flags *metaArithmeticFlags) (*request, *response) {
	if flags.b {
		key = base64Encode(key)
	}
	flags.D = delta

	b := newProtocolBuilder().
		AddString("ma").
		AddBytes(key)

	b.AddFlagBool("b", flags.b)
	b.AddFlagUint("C", flags.C)
	b.AddFlagUint("E", flags.E)
	b.AddFlagUint("N", flags.N)
	b.AddFlagUint("J", flags.J)
	b.AddFlagUint("D", flags.D)
	b.AddFlagUint("T", flags.T)
	b.AddFlagString("M", string(flags.M))
	b.AddFlagUint("O", flags.O)
	b.AddFlagBool("q", flags.q)
	b.AddFlagBool("t", flags.t)
	b.AddFlagBool("c", flags.c)
	b.AddFlagBool("v", flags.v)
	b.AddFlagBool("k", flags.k)

	raw := b.AddCRLF().build()

	req := &request{
		cmd: []byte("ma"),
		key: []byte(key),
		raw: raw,
	}

	var resp *response
	if flags.q {
		resp = buildNoReplyResponse()
	} else if flags.v {
		resp = buildLimitedLineResponse(2)
	} else {
		resp = buildLimitedLineResponse(1)
	}

	return req, resp
}
