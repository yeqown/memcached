package memcached

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strconv"
	"time"

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
func buildStorageCommand(command, key string, value []byte, flags uint32, exptime time.Duration, noReply bool) (*request, *response) {
	b := newProtocolBuilder().
		AddString(command).
		AddString(key).                     // key
		AddUint(uint64(flags)).             // flags
		AddUint(uint64(exptime.Seconds())). // exptime
		AddInt(len(value))                  // bytes
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
func buildTouchCommand(key string, expTime time.Duration, noReply bool) (*request, *response) {
	b := newProtocolBuilder().
		AddString("touch").
		AddString(key).
		AddUint(uint64(expTime.Seconds()))
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
	key string, value []byte, flags uint32, expTime time.Duration, casUnique uint64, noReply bool) (*request, *response) {
	b := newProtocolBuilder().
		AddString("cas").                   // command
		AddString(key).                     // key
		AddUint(uint64(flags)).             // flags
		AddUint(uint64(expTime.Seconds())). // exptime
		AddInt(len(value)).                 // bytes
		AddUint(casUnique)                  // cas unique
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
func buildGetAndTouchesCommand(command string, expiry time.Duration, keys ...string) (*request, *response) {
	b := newProtocolBuilder().
		AddString(command)
	defer b.release()

	for _, key := range keys {
		b.AddString(key)
	}

	b.AddUint(uint64(expiry.Seconds())).
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

// Statistic represents the statistics of the memcached server.
type Statistic struct {
	PID                    int64   `json:"pid"`
	Uptime                 int64   `json:"uptime"` // seconds since the server started
	Time                   int64   `json:"time"`   // unix timestamp
	Version                string  `json:"version"`
	Libevent               string  `json:"libevent"`
	PointerSize            int     `json:"pointer_size"`
	RusageUser             float64 `json:"rusage_user"`   // seconds.microseconds
	RusageSystem           float64 `json:"rusage_system"` // seconds.microseconds
	MaxConnections         int64   `json:"max_connections"`
	CurrConnections        int64   `json:"curr_connections"`
	TotalConnections       int64   `json:"total_connections"`
	RejectedConnections    int64   `json:"rejected_connections"`
	ConnectionStructures   int64   `json:"connection_structures"`
	ResponseObjOOM         int64   `json:"response_obj_oom"`
	ResponseObjCount       int64   `json:"response_obj_count"`
	ResponseObjBytes       int64   `json:"response_obj_bytes"`
	ReadBufCount           int64   `json:"read_buf_count"`
	ReadBufBytes           int64   `json:"read_buf_bytes"`
	ReadBufBytesFree       int64   `json:"read_buf_bytes_free"`
	ReadBufOOM             int64   `json:"read_buf_oom"`
	ReservedFDs            int64   `json:"reserved_fds"`
	CmdGet                 int64   `json:"cmd_get"`
	CmdSet                 int64   `json:"cmd_set"`
	CmdFlush               int64   `json:"cmd_flush"`
	CmdTouch               int64   `json:"cmd_touch"`
	CmdMeta                int64   `json:"cmd_meta"`
	GetHits                int64   `json:"get_hits"`
	GetMisses              int64   `json:"get_misses"`
	GetExpired             int64   `json:"get_expired"`
	GetFlushed             int64   `json:"get_flushed"`
	DeleteMisses           int64   `json:"delete_misses"`
	DeleteHits             int64   `json:"delete_hits"`
	IncrMisses             int64   `json:"incr_misses"`
	IncrHits               int64   `json:"incr_hits"`
	DecrMisses             int64   `json:"decr_misses"`
	DecrHits               int64   `json:"decr_hits"`
	CasMisses              int64   `json:"cas_misses"`
	CasHits                int64   `json:"cas_hits"`
	CasBadval              int64   `json:"cas_badval"`
	TouchHits              int64   `json:"touch_hits"`
	TouchMisses            int64   `json:"touch_misses"`
	StoreTooLarge          int64   `json:"store_too_large"`
	StoreNoMemory          int64   `json:"store_no_memory"`
	AuthCmds               int64   `json:"auth_cmds"`
	AuthErrors             int64   `json:"auth_errors"`
	BytesRead              int64   `json:"bytes_read"`
	BytesWritten           int64   `json:"bytes_written"`
	LimitMaxbytes          int64   `json:"limit_maxbytes"`
	AcceptingConns         bool    `json:"accepting_conns"`
	ListenDisabledNum      int64   `json:"listen_disabled_num"`
	TimeInListenDisabledUs int64   `json:"time_in_listen_disabled_us"`
	Threads                int64   `json:"threads"`
	ConnYields             int64   `json:"conn_yields"`
	HashPowerLevel         int64   `json:"hash_power_level"`
	HashBytes              int64   `json:"hash_bytes"`
	HashIsExpanding        bool    `json:"hash_is_expanding"`

	SlabReassignRescues        int64 `json:"slab_reassign_rescues"`
	SlabReassignChunkRescues   int64 `json:"slab_reassign_chunk_rescues"`
	SlabReassignEvictionsNomem int64 `json:"slab_reassign_evictions_nomem"`
	SlabReassignInlineReclaim  int64 `json:"slab_reassign_inline_reclaim"`
	SlabReassignBusyItems      int64 `json:"slab_reassign_busy_items"`
	SlabReassignBusyDeletes    int64 `json:"slab_reassign_busy_deletes"`
	SlabReassignRunning        int64 `json:"slab_reassign_running"`
	SlabsMoved                 int64 `json:"slabs_moved"`

	LruCrawlerRunning    int64 `json:"lru_crawler_running"`
	LruCrawlerStarts     int64 `json:"lru_crawler_starts"`
	LruMaintainerJuggles int64 `json:"lru_maintainer_juggles"`
	MallocFails          int64 `json:"malloc_fails"`
	LogWorkerDropped     int64 `json:"log_worker_dropped"`
	LogWorkerWritten     int64 `json:"log_worker_written"`
	LogWatcherSkipped    int64 `json:"log_watcher_skipped"`
	LogWatcherSent       int64 `json:"log_watcher_sent"`
	LogWatchers          int64 `json:"log_watchers"`
	UnexpectedNapiIDs    int64 `json:"unexpected_napi_ids"`
	RoundRobinFallback   int64 `json:"round_robin_fallback"`
	Bytes                int64 `json:"bytes"`
	CurrItems            int64 `json:"curr_items"`
	TotalItems           int64 `json:"total_items"`
	SlabGlobalPagePool   int64 `json:"slab_global_page_pool"`
	ExpiredUnfetched     int64 `json:"expired_unfetched"`
	EvictedUnfetched     int64 `json:"evicted_unfetched"`
	EvictedActive        int64 `json:"evicted_active"`
	Evictions            int64 `json:"evictions"`
	Reclaimed            int64 `json:"reclaimed"`
	CrawlerReclaimed     int64 `json:"crawler_reclaimed"`
	CrawlerItemsChecked  int64 `json:"crawler_items_checked"`
	LrutailReflocked     int64 `json:"lrutail_reflocked"`

	MovesToCold    int64 `json:"moves_to_cold"`
	MovesToWarm    int64 `json:"moves_to_warm"`
	MovesWithinLRU int64 `json:"moves_within_lru"`

	DirectReclaims  int64 `json:"direct_reclaims"`
	LruBumpsDropped int64 `json:"lru_bumps_dropped"`
}

func parseStats(lines [][]byte) (*Statistic, error) {
	if len(lines) <= 0 {
		return nil, errors.Wrap(ErrMalformedResponse, "empty response")
	}

	transitionMap := make(map[string]any, len(lines))
	for _, line := range lines {
		// STAT <key> <value>\r\n
		fields := bytes.Fields(bytes.TrimSuffix(line, _CRLFBytes))
		if len(fields) != 3 || !bytes.Equal(fields[0], []byte("STAT")) {
			continue
		}

		key := string(fields[1])

		// parse the value into int64 as default, but there are some exceptions:
		// string: version, libevent
		// float: rusage_user, rusage_system
		switch key {
		case "version", "libevent":
			transitionMap[key] = string(fields[2])
		case "rusage_user", "rusage_system":
			v, err := strconv.ParseFloat(string(fields[2]), 64)
			if err != nil {
				slog.Warn("memcached: parse float failed",
					"key", key,
					"value", string(fields[2]),
					"err", err,
				)
				continue
			}
			transitionMap[key] = v
		case "hash_is_expanding", "accepting_conns":
			v, err := strconv.ParseBool(string(fields[2]))
			if err != nil {
				slog.Warn("memcached: parse bool failed",
					"key", key,
					"value", string(fields[2]),
					"err", err,
				)
				continue
			}
			transitionMap[key] = v
		default:
			v, err := strconv.ParseInt(string(fields[2]), 10, 64)
			if err != nil {
				slog.Warn("memcached: parse int failed",
					"key", key,
					"value", string(fields[2]),
					"err", err,
				)
				continue
			}
			transitionMap[key] = v
		}
	}

	raw, err := json.Marshal(transitionMap)
	if err != nil {
		return nil, errors.Wrap(err, "parseStats marshal transition map failed")
	}

	stat := &Statistic{}
	if err = json.Unmarshal(raw, stat); err != nil {
		return nil, errors.Wrap(err, "parseStats unmarshal failed")
	}

	return stat, nil
}

func buildStatsCommand(subCommand string) (*request, *response) {
	b := newProtocolBuilder().
		AddString("stats")

	defer b.release()

	if subCommand != "" {
		b.AddString(subCommand)
	}

	raw := b.AddCRLF().
		build()

	req := buildRequest([]byte("stats"), nil, raw)
	resp := buildSpecEndLineResponse(_EndCRLFBytes, 64)

	return req, resp
}

//nolint:unused
func buildRawCommand(rawCommand string, indicator responseEndIndicator, lines int) (*request, *response) {
	_, _, _ = rawCommand, indicator, lines
	panic("IMPLEMENT ME!!!")
}
