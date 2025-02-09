package hash

// HashFunc 定义了哈希函数的接口
type HashFunc interface {
	Hash(key []byte) uint64
}