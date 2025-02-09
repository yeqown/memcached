package hash

import "hash/crc32"

type CRC32 struct{}

func NewCRC32() *CRC32 {
	return &CRC32{}
}

func (h *CRC32) Hash(key []byte) uint64 {
	return uint64(crc32.ChecksumIEEE(key))
}