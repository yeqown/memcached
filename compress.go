package memcached

import (
	"bytes"
	"compress/flate"
	"io"

	"github.com/pkg/errors"
)

const (
	defaultCompressionThreshold = 1024 // 1KB
	mcFlagsMagic                = uint32(0xA)
)

// CompressionAlgorithm identifies the compression algorithm encoded in MC-FLAGS.
type CompressionAlgorithm uint8

const (
	// CompressionAlgorithmNone disables compression for the stored value.
	CompressionAlgorithmNone CompressionAlgorithm = 0x0
	// CompressionAlgorithmDeflate uses DEFLATE compression.
	CompressionAlgorithmDeflate CompressionAlgorithm = 0x1
)

// MCFlags is the 32-bit semantic flag word encoded by the MC-COMPRESS spec.
type MCFlags uint32

func (f MCFlags) Raw() uint32 {
	return uint32(f)
}

func (f MCFlags) IsValid() bool {
	if !f.IsMCFlags() {
		return true
	}
	return isSupportedCompressionAlgorithm(f.CompressionAlgorithm())
}

func (f MCFlags) IsMCFlags() bool {
	return ((uint32(f) >> 28) & 0xF) == mcFlagsMagic
}

func (f MCFlags) IsCompressed() bool {
	return f.CompressionAlgorithm() != CompressionAlgorithmNone
}

func (f MCFlags) CompressionAlgorithm() CompressionAlgorithm {
	if !f.IsMCFlags() {
		return CompressionAlgorithmNone
	}
	return CompressionAlgorithm((uint32(f) >> 24) & 0xF)
}

func (f MCFlags) AppFlags() uint16 {
	if !f.IsMCFlags() {
		return uint16(uint32(f) & 0xFFFF)
	}
	return uint16((uint32(f) >> 8) & 0xFFFF)
}

func (f MCFlags) Reserved() uint8 {
	if !f.IsMCFlags() {
		return 0
	}
	return uint8(uint32(f) & 0xFF)
}

func isSupportedCompressionAlgorithm(algorithm CompressionAlgorithm) bool {
	switch algorithm {
	case CompressionAlgorithmNone, CompressionAlgorithmDeflate:
		return true
	default:
		return false
	}
}

func compress(src []byte, algorithm CompressionAlgorithm) ([]byte, error) {
	switch algorithm {
	case CompressionAlgorithmNone:
		return src, nil
	case CompressionAlgorithmDeflate:
		var buf bytes.Buffer
		writer, err := flate.NewWriter(&buf, flate.DefaultCompression)
		if err != nil {
			return nil, errors.Wrap(err, "create deflate writer")
		}
		if _, err = writer.Write(src); err != nil {
			_ = writer.Close()
			return nil, errors.Wrap(err, "write deflate payload")
		}
		if err = writer.Close(); err != nil {
			return nil, errors.Wrap(err, "close deflate writer")
		}
		return buf.Bytes(), nil
	default:
		return nil, errors.Wrap(ErrNotSupported, "compression algorithm")
	}
}

func decompress(src []byte, algorithm CompressionAlgorithm) ([]byte, error) {
	switch algorithm {
	case CompressionAlgorithmNone:
		return src, nil
	case CompressionAlgorithmDeflate:
		reader := flate.NewReader(bytes.NewReader(src))
		defer reader.Close()

		payload, err := io.ReadAll(reader)
		if err != nil {
			return nil, errors.Wrap(err, "read deflate payload")
		}
		return payload, nil
	default:
		return nil, errors.Wrap(ErrNotSupported, "compression algorithm")
	}
}

func buildMCFlags(appFlags uint16, algorithm CompressionAlgorithm) (MCFlags, error) {
	if !isSupportedCompressionAlgorithm(algorithm) {
		return 0, errors.Wrap(ErrNotSupported, "compression algorithm")
	}

	return MCFlags(((mcFlagsMagic & 0xF) << 28) |
		((uint32(algorithm) & 0xF) << 24) |
		(uint32(appFlags) << 8)), nil
}

func prepareStorageValue(value []byte, appFlags uint16, compressAlg CompressionAlgorithm, compressionThreshold int) ([]byte, MCFlags, error) {
	flag0, err := buildMCFlags(appFlags, CompressionAlgorithmNone)
	if err != nil {
		return nil, 0, err
	}
	if compressAlg == CompressionAlgorithmNone {
		return value, flag0, nil
	}
	if len(value) < compressionThreshold {
		return value, flag0, nil
	}

	compressed, err := compress(value, compressAlg)
	if err != nil {
		return nil, 0, err
	}
	if len(compressed) >= len(value) {
		return value, flag0, nil
	}

	flags1, err := buildMCFlags(appFlags, compressAlg)
	if err != nil {
		return nil, 0, err
	}

	return compressed, flags1, nil
}

func tryDecompressValue(value []byte, flags MCFlags) ([]byte, error) {
	if !flags.IsMCFlags() {
		return value, nil
	}
	if !flags.IsValid() {
		return nil, errors.Wrap(ErrNotFound, "unknown compression algorithm")
	}
	if !flags.IsCompressed() {
		return value, nil
	}

	decoded, err := decompress(value, flags.CompressionAlgorithm())
	if err != nil {
		return nil, errors.Wrap(ErrNotFound, "decompression failed")
	}
	return decoded, nil
}
