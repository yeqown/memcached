package memcached

import (
	"bytes"
	"compress/flate"
	"io"

	"github.com/pkg/errors"
)

const defaultCompressionThreshold = 1024 // 1KB

// CompressionAlgorithm identifies the compression algorithm encoded in MC-FLAGS.
type CompressionAlgorithm uint8

const (
	// CompressionAlgorithmNone disables compression for the stored value.
	CompressionAlgorithmNone CompressionAlgorithm = 0x0
	// CompressionAlgorithmDeflate uses DEFLATE compression.
	CompressionAlgorithmDeflate CompressionAlgorithm = 0x1
)

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

		payload, err := io.ReadAll(reader)
		closeErr := reader.Close()
		if err != nil {
			return nil, errors.Wrap(err, "read deflate payload")
		}
		if closeErr != nil {
			return nil, errors.Wrap(closeErr, "close deflate reader")
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
	if flags.unconventional() || !flags.isCompressed() {
		return value, nil
	}

	decoded, err := decompress(value, flags.compressionAlgorithm())
	if err != nil {
		return nil, errors.Wrap(ErrNotFound, "decompression failed")
	}
	return decoded, nil
}
