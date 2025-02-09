package hash

const (
	c1 = uint64(0x87c37b91114253d5)
	c2 = uint64(0x4cf5ad432745937f)
)

type Murmur3 struct {
	seed uint64
}

func NewMurmur3(seed uint64) *Murmur3 {
	return &Murmur3{seed: seed}
}

func (h *Murmur3) Hash(key []byte) uint64 {
	length := len(key)
	hash := h.seed

	// 处理主体部分
	nblocks := length / 8
	for i := 0; i < nblocks; i++ {
		k := uint64(key[i*8]) | uint64(key[i*8+1])<<8 |
			uint64(key[i*8+2])<<16 | uint64(key[i*8+3])<<24 |
			uint64(key[i*8+4])<<32 | uint64(key[i*8+5])<<40 |
			uint64(key[i*8+6])<<48 | uint64(key[i*8+7])<<56

		k *= c1
		k = (k << 31) | (k >> 33)
		k *= c2

		hash ^= k
		hash = (hash << 27) | (hash >> 37)
		hash = hash*5 + 0x52dce729
	}

	// 处理剩余字节
	tail := key[nblocks*8:]
	k2 := uint64(0)
	switch length & 7 {
	case 7:
		k2 ^= uint64(tail[6]) << 48
		fallthrough
	case 6:
		k2 ^= uint64(tail[5]) << 40
		fallthrough
	case 5:
		k2 ^= uint64(tail[4]) << 32
		fallthrough
	case 4:
		k2 ^= uint64(tail[3]) << 24
		fallthrough
	case 3:
		k2 ^= uint64(tail[2]) << 16
		fallthrough
	case 2:
		k2 ^= uint64(tail[1]) << 8
		fallthrough
	case 1:
		k2 ^= uint64(tail[0])
		k2 *= c1
		k2 = (k2 << 31) | (k2 >> 33)
		k2 *= c2
		hash ^= k2
	}

	// 最终混淆
	hash ^= uint64(length)
	hash ^= hash >> 33
	hash *= 0xff51afd7ed558ccd
	hash ^= hash >> 33
	hash *= 0xc4ceb9fe1a85ec53
	hash ^= hash >> 33

	return hash
}