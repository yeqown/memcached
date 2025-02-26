package memcached

import (
	"bytes"
	"strconv"

	"github.com/pkg/errors"
)

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

func (i Item) String() string {
	return "Item{" +
		"Key:" + i.Key +
		" Value:" + string(i.Value) +
		" Flags:" + strconv.FormatUint(uint64(i.Flags), 10) +
		" CAS:" + strconv.FormatUint(i.CAS, 10) +
		"}"
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

func (m MetaItem) String() string {
	return "MetaItem{" +
		"Key:" + string(m.Key) +
		" Value:" + string(m.Value) +
		" CAS:" + strconv.FormatUint(m.CAS, 10) +
		" Flags:" + strconv.FormatUint(uint64(m.Flags), 10) +
		" TTL:" + strconv.FormatInt(m.TTL, 10) +
		" LastAccessedTime:" + strconv.FormatInt(m.LastAccessedTime, 10) +
		" Size:" + strconv.FormatUint(m.Size, 10) +
		" Opaque:" + strconv.FormatUint(m.Opaque, 10) +
		" HitBefore:" + strconv.FormatBool(m.HitBefore) +
		"}"
}

// MetaItemDebug represents a key-value pair with meta information for debug.
//
//	exp   = expiration time
//	la    = time in seconds since last access
//	cas   = CAS ID
//	fetch = whether an item has been fetched before
//	cls   = slab class id
//	size  = total size in bytes
type MetaItemDebug struct {
	Key            []byte // key
	TTL            int64  // exp, expiration time in seconds, -1 means never expire
	LastAssessTime int64  // la, time in seconds since last access
	CAS            uint64 // cas
	HitBefore      bool   // fetch
	SlabClassID    uint64 // cls
	Size           uint64 // size
}

func buildVersionCommand() (*request, *response) {
	req := buildRequest([]byte("version"), nil, []byte("version\r\n"))
	resp := buildLimitedLineResponse(1)
	return req, resp
}

func buildFlushAllCommand(noReply bool) (*request, *response) {
	req := buildRequest([]byte("flush_all"), nil, []byte("flush_all\r\n"))

	var resp *response
	if noReply {
		resp = buildNoReplyResponse()
	} else {
		resp = buildLimitedLineResponse(1)
	}

	return req, resp
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
	defer b.release()

	if noReply {
		b.AddBytes(_NoReplyBytes)
	}

	raw := b.AddCRLF().
		AddBytes(value). // data block
		AddCRLF().
		build()

	req := buildRequest([]byte(command), []byte(key), raw)

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
	defer b.release()

	if noReply {
		b.AddBytes(_NoReplyBytes)
	}

	req := buildRequest([]byte("delete"), []byte(key), b.AddCRLF().build())

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
	defer b.release()

	if noReply {
		b.AddBytes(_NoReplyBytes)
	}

	req := buildRequest([]byte("touch"), []byte(key), b.AddCRLF().build())

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
	defer b.release()

	if noReply {
		b.AddBytes(_NoReplyBytes)
	}

	raw := b.AddCRLF().
		AddBytes(value). // data block
		AddCRLF().
		build()

	req := buildRequest([]byte("cas"), []byte(key), raw)

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
	defer b.release()

	for _, key := range keys {
		b.AddString(key)
	}
	b.AddCRLF()

	req := buildRequest([]byte(command), nil, b.build())
	resp := buildSpecEndLineResponse(_EndCRLFBytes, len(keys)*2+1)

	return req, resp
}

// buildGetAndTouchCommand constructs get and touch command.
// gat/gats <key> <exptime>\r\n
func buildGetAndTouchesCommand(command string, expiry uint32, keys ...string) (*request, *response) {
	b := newProtocolBuilder().
		AddString(command)
	defer b.release()

	for _, key := range keys {
		b.AddString(key)
	}

	b.AddUint(uint64(expiry)).
		AddCRLF()

	req := buildRequest([]byte(command), nil, b.build())
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
func parseValueItems(lines [][]byte, withoutEndLine, withCAS bool) (_ []*Item, err error) {
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
		rn      = n
		items   = make([]*Item, 0, (n/2)+1) // pre-alloc to avoid memory allocation
		dataLen uint64
	)

	if !withoutEndLine {
		// avoid iterate the last line "END\r\n"
		rn = n - 1
	}

	for i := 0; i < rn; i += 2 {
		line := trimCRLF(lines[i])
		if !bytes.HasPrefix(line, _ValueBytes) {
			continue
		}

		item := &Item{
			Key:   "",
			Value: nil,
			Flags: 0,
			CAS:   0,
		}
		dataLen, err = parseValueLine(line, item, withCAS)
		if err != nil {
			return nil, err
		}

		// Read the data block
		if i+1 >= n {
			return nil, errors.Wrap(ErrMalformedResponse, "missing data block")
		}
		item.Value = trimCRLF(lines[i+1])
		if len(item.Value) != int(dataLen) {
			return nil, errors.Wrap(ErrMalformedResponse, "data block length mismatch")
		}

		items = append(items, item)
	}

	return items, nil
}

// parseValueLine extract item from VALUE line, like following:
// VALUE <key> <flags> <bytes> <cas unique> => Item{key, flags, cas}
//
// if withCas is false, VALUE line is:
// VALUE <key> <flags> <bytes> => Item{key, flags, 0}
func parseValueLine(line []byte, item *Item, withCas bool) (dataLen uint64, err error) {
	const (
		keyIndex     = 1
		flagsIndex   = 2
		dataLenIndex = 3
		casIndex     = 4
	)

	n := len(line)
	start := len(_ValueBytes)
	fieldStart := start
	nField := 0

	for i := start; i < n; i++ {
		if nField > 5 || (!withCas && nField > 4) {
			return 0, errors.Wrap(ErrMalformedResponse, "invalid VALUE line")
		}

		if line[i] != ' ' && i != n-1 {
			continue
		}

		// another field starts from fieldStart to i, or the last field
		// the 'i' is the index of space or the last byte.
		switch nField {
		case keyIndex:
			item.Key = unsafeByteSliceToString(line[fieldStart:i])
		case flagsIndex:
			flags, err := parseUintFromBytes(line[fieldStart:i])
			if err != nil {
				return 0, errors.Wrap(ErrMalformedResponse, "invalid flags")
			}
			item.Flags = uint32(flags)
		case dataLenIndex:
			si := i
			if i == n-1 {
				si = i + 1
			}
			dataLen, err = parseUintFromBytes(line[fieldStart:si])
			if err != nil {
				return 0, errors.Wrap(ErrMalformedResponse, "invalid data length")
			}
		case casIndex:
			si := i
			if i == n-1 {
				si = i + 1
			}
			item.CAS, err = parseUintFromBytes(line[fieldStart:si])
			if err != nil {
				return 0, errors.Wrap(ErrMalformedResponse, "invalid CAS")
			}
		}

		fieldStart = i + 1
		nField++
	}

	return dataLen, nil
}

// parseUintFromBytes parses slice of bytes to uint64.
//
// For example:
// []byte("1234567890") -> 1234567890
//
// If the slice of bytes is empty, it returns 0.
//
// If the slice of bytes contains non-digit characters, it returns an error.
func parseUintFromBytes(bs []byte) (uint64, error) {
	if len(bs) == 0 {
		return 0, nil
	}

	r := uint64(0)
	for _, b := range bs {
		if b < '0' || b > '9' {
			return 0, errors.Wrap(ErrMalformedResponse, "invalid uint number")
		}

		r = r*10 + uint64(b-'0')
	}

	return r, nil
}

// incr/decr <key> <value> [noreply]\r\n
func buildArithmeticCommand(command, key string, delta uint64, noReply bool) (*request, *response) {
	b := newProtocolBuilder().
		AddString(command).
		AddString(key).
		AddUint(delta)
	defer b.release()

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
