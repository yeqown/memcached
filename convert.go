package memcached

import (
	"encoding/binary"
	"math"
)

// uint64ToUint32 converts v to uint32 when it fits.
func uint64ToUint32(v uint64) (uint32, bool) {
	if v > math.MaxUint32 {
		return 0, false
	}
	var b [8]byte
	binary.BigEndian.PutUint64(b[:], v)
	return binary.BigEndian.Uint32(b[4:]), true
}

// uint64ToInt64 converts v to int64 when it fits in a non-negative int64.
func uint64ToInt64(v uint64) (int64, bool) {
	if v > uint64(math.MaxInt64) {
		return 0, false
	}
	var b [8]byte
	binary.BigEndian.PutUint64(b[:], v)
	// Assemble from bytes so the conversion is not a truncating uint64->int64 cast.
	return int64(b[0])<<56 | int64(b[1])<<48 | int64(b[2])<<40 | int64(b[3])<<32 |
		int64(b[4])<<24 | int64(b[5])<<16 | int64(b[6])<<8 | int64(b[7]), true
}

// intToUint16 converts a non-negative length to uint16 when it fits.
func intToUint16(n int) (uint16, bool) {
	if n < 0 || n > math.MaxUint16 {
		return 0, false
	}
	var b [8]byte
	binary.BigEndian.PutUint64(b[:], uint64(n))
	return binary.BigEndian.Uint16(b[6:]), true
}

// intToUint32 converts a non-negative length to uint32 when it fits.
func intToUint32(n int) (uint32, bool) {
	if n < 0 || n > math.MaxUint32 {
		return 0, false
	}
	var b [8]byte
	binary.BigEndian.PutUint64(b[:], uint64(n))
	return binary.BigEndian.Uint32(b[4:]), true
}

// intToUint8 converts a non-negative length to uint8 when it fits.
func intToUint8(n int) (uint8, bool) {
	if n < 0 || n > math.MaxUint8 {
		return 0, false
	}
	var b [8]byte
	binary.BigEndian.PutUint64(b[:], uint64(n))
	return b[7], true
}
