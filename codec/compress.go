package codec

import (
	"bytes"
	"compress/zlib"
	"errors"
	"io"
	"log"

	"github.com/golang/snappy"
	"github.com/klauspost/compress/zstd"
	"github.com/pierrec/lz4/v4"
)

var (
	errNotSupported = errors.New("compression algorithm: not supported")
	errInvalidValue = errors.New("decompressed payload too large")
	errInvalidFlags = errors.New("app flag exceed MC-COMPRESS 16-bit range")
	errNotFound     = errors.New("decompression failed")
)

const (
	// The minimal size to compress is 10KB which is used to avoid negative optimization maybe cpu cost more than reduced size.
	defaultCompressionThreshold = 10240
	defaultCompressionLevel     = 6

	maxDecompressedValueSize     = 128 << 20 // 128MB
	maxCompressionExpansionRatio = 100       // 100x compression expansion ratio

	conventionalFlagsMagic = uint32(0xA)
)

// Compression identifies the payload algorithm encoded in MC-COMPRESS flags.
type Compression uint8

const (
	CompressionAlgorithmNone    Compression = 0x0
	CompressionAlgorithmDeflate Compression = 0x1
	CompressionAlgorithmLZ4     Compression = 0x2
	CompressionAlgorithmSnappy  Compression = 0x3
	CompressionAlgorithmZstd    Compression = 0x4
)

// compressCodec stores compressed values using the MC-COMPRESS flag layout.
//
// The high nibble marks conventional MC-COMPRESS flags, the next nibble stores
// the compression algorithm, and the 16-bit app flag field is preserved for
// callers. Values are only stored compressed when they meet the threshold and
// the compressed payload is smaller than the original.
type compressCodec struct {
	compression      Compression
	compressionLevel int
	threshold        int
}

// NewCompressCodec creates a codec that stores values with MC-COMPRESS flags.
func NewCompressCodec(algorithm Compression, threshold int) *compressCodec {
	switch algorithm {
	case CompressionAlgorithmNone, CompressionAlgorithmDeflate, CompressionAlgorithmLZ4, CompressionAlgorithmSnappy, CompressionAlgorithmZstd:
	default:
		algorithm = CompressionAlgorithmNone
	}

	if threshold <= 0 {
		threshold = defaultCompressionThreshold
	}
	return &compressCodec{
		compression:      algorithm,
		compressionLevel: defaultCompressionLevel,
		threshold:        threshold,
	}
}

func (c *compressCodec) Encode(_ []byte, value []byte, flag uint32) ([]byte, uint32, error) {
	if flag > 0xFFFF {
		return nil, 0, errInvalidFlags
	}

	uncompressedFlag := newCompressFlag(flag, CompressionAlgorithmNone)

	// No need to compress:
	// - not select compression algorithm
	// - not satisfy the compression threshold
	if c.compression == CompressionAlgorithmNone || len(value) < c.threshold {
		return value, uint32(uncompressedFlag), nil
	}

	compressed, err := compress(value, c.compression, c.compressionLevel)
	if err != nil {
		return nil, 0, err
	}
	if len(compressed) >= len(value) {
		return value, uint32(uncompressedFlag), nil
	}

	flag = uint32(newCompressFlag(flag, c.compression))
	return compressed, flag, nil
}

func (c *compressCodec) Decode(key, value []byte, flags uint32) ([]byte, uint32, error) {
	cflag := compressFlag(flags)
	if cflag.unconventional() || !cflag.isCompressed() {
		return value, uint32(cflag), nil
	}

	algorithm := cflag.compressionAlgorithm()
	decoded, err := decompress(value, algorithm)
	if err != nil {
		log.Printf("memcached: decompression failed: key=%q car_id=%d err=%v", string(key), uint8(algorithm), err)
		return nil, 0, errNotFound
	}

	return decoded, uint32(cflag), nil
}

func (c *compressCodec) SupportsOperation(operation string) error {
	if c.compression == CompressionAlgorithmNone {
		return nil
	}
	if operation == "append" || operation == "prepend" {
		return errNotSupported
	}
	return nil
}

func compress(src []byte, algorithm Compression, level int) ([]byte, error) {
	switch algorithm {
	case CompressionAlgorithmNone:
		return src, nil
	case CompressionAlgorithmDeflate:
		return compressDeflate(src, level)
	case CompressionAlgorithmLZ4:
		return compressLZ4(src, level)
	case CompressionAlgorithmSnappy:
		return compressSnappy(src, level)
	case CompressionAlgorithmZstd:
		return compressZstd(src, level)
	default:
		return nil, errNotSupported
	}
}

func compressDeflate(src []byte, level int) ([]byte, error) {
	var buf bytes.Buffer
	writer, err := zlib.NewWriterLevel(&buf, level)
	if err != nil {
		return nil, err
	}
	if _, err = writer.Write(src); err != nil {
		_ = writer.Close()
		return nil, err
	}
	if err = writer.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func compressLZ4(src []byte, level int) ([]byte, error) {
	var buf bytes.Buffer
	writer := lz4.NewWriter(&buf)
	if err := writer.Apply(lz4.CompressionLevelOption(lz4CompressionLevel(level))); err != nil {
		return nil, err
	}
	if _, err := writer.Write(src); err != nil {
		_ = writer.Close()
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func lz4CompressionLevel(level int) lz4.CompressionLevel {
	levels := [...]lz4.CompressionLevel{
		lz4.Fast,
		lz4.Level1,
		lz4.Level2,
		lz4.Level3,
		lz4.Level4,
		lz4.Level5,
		lz4.Level6,
		lz4.Level7,
		lz4.Level8,
		lz4.Level9,
	}
	if level < 0 {
		return lz4.Fast
	}
	if level >= len(levels) {
		return lz4.Level9
	}
	return levels[level]
}

func compressSnappy(src []byte, _ int) ([]byte, error) {
	return snappy.Encode(nil, src), nil
}

func compressZstd(src []byte, level int) ([]byte, error) {
	var buf bytes.Buffer
	writer, err := zstd.NewWriter(&buf, zstd.WithEncoderLevel(zstd.EncoderLevelFromZstd(level)))
	if err != nil {
		return nil, err
	}
	if _, err = writer.Write(src); err != nil {
		writer.Close()
		return nil, err
	}
	if err = writer.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func decompress(src []byte, algorithm Compression) ([]byte, error) {
	switch algorithm {
	case CompressionAlgorithmNone:
		return src, nil
	case CompressionAlgorithmDeflate:
		return decompressDeflate(src)
	case CompressionAlgorithmLZ4:
		return decompressLZ4(src)
	case CompressionAlgorithmSnappy:
		return decompressSnappy(src)
	case CompressionAlgorithmZstd:
		return decompressZstd(src)
	default:
		return nil, errNotSupported
	}
}

func decompressDeflate(src []byte) ([]byte, error) {
	reader, err := zlib.NewReader(bytes.NewReader(src))
	if err != nil {
		return nil, err
	}
	limit := decompressedValueSizeLimit(len(src))
	payload, err := io.ReadAll(io.LimitReader(reader, limit+1))
	closeErr := reader.Close()
	if err != nil {
		return nil, err
	}
	if closeErr != nil {
		return nil, closeErr
	}
	if int64(len(payload)) > limit {
		return nil, errInvalidValue
	}
	return payload, nil
}

func decompressLZ4(src []byte) ([]byte, error) {
	return readCompressed(lz4.NewReader(bytes.NewReader(src)), len(src))
}

func decompressSnappy(src []byte) ([]byte, error) {
	payload, err := snappy.Decode(nil, src)
	if err != nil {
		return nil, err
	}
	if int64(len(payload)) > decompressedValueSizeLimit(len(src)) {
		return nil, errInvalidValue
	}
	return payload, nil
}

func decompressZstd(src []byte) ([]byte, error) {
	reader, err := zstd.NewReader(bytes.NewReader(src))
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	return readCompressed(reader, len(src))
}

func readCompressed(reader io.Reader, compressedSize int) ([]byte, error) {
	limit := decompressedValueSizeLimit(compressedSize)
	payload, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(payload)) > limit {
		return nil, errInvalidValue
	}
	return payload, nil
}

func decompressedValueSizeLimit(compressedSize int) int64 {
	limit := int64(compressedSize) * maxCompressionExpansionRatio
	if limit > maxDecompressedValueSize {
		return maxDecompressedValueSize
	}
	return limit
}

type compressFlag uint32

func newCompressFlag(appFlags uint32, algorithm Compression) compressFlag {
	return compressFlag(((conventionalFlagsMagic & 0xF) << 28) | ((uint32(algorithm) & 0xF) << 24) | (appFlags << 8))
}

func IsUnconventional(flag uint32) bool {
	return compressFlag(flag).unconventional()
}

func AppFlags(flag uint32) uint32 {
	return compressFlag(flag).appFlags()
}

func IsCompressed(flag uint32) bool {
	return compressFlag(flag).isCompressed()
}

func (f compressFlag) unconventional() bool {
	return ((uint32(f) >> 28) & 0xF) != conventionalFlagsMagic
}

func (f compressFlag) appFlags() uint32 {
	if f.unconventional() {
		return uint32(f)
	}
	return (uint32(f) >> 8) & 0xFFFF
}

func (f compressFlag) isCompressed() bool {
	return f.compressionAlgorithm() != CompressionAlgorithmNone
}

func (f compressFlag) compressionAlgorithm() Compression {
	if f.unconventional() {
		return CompressionAlgorithmNone
	}
	return Compression((uint32(f) >> 24) & 0xF)
}
