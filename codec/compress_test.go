package codec

import (
	"bytes"
	"compress/flate"
	"compress/zlib"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCompressCodecRoundTrip(t *testing.T) {
	src := bytes.Repeat([]byte("hello hello hello hello hello hello random-ish padding "), 8)

	tests := []struct {
		name       string
		algorithm  Compression
		level      int
		compressed bool
	}{
		{name: "none", algorithm: CompressionAlgorithmNone, level: 0},
		{name: "deflate huffman only", algorithm: CompressionAlgorithmDeflate, level: flate.HuffmanOnly, compressed: true},
		{name: "deflate default", algorithm: CompressionAlgorithmDeflate, level: flate.DefaultCompression, compressed: true},
		{name: "deflate no compression", algorithm: CompressionAlgorithmDeflate, level: flate.NoCompression},
		{name: "deflate best speed", algorithm: CompressionAlgorithmDeflate, level: flate.BestSpeed, compressed: true},
		{name: "deflate best compression", algorithm: CompressionAlgorithmDeflate, level: flate.BestCompression, compressed: true},
		{name: "lz4 fast", algorithm: CompressionAlgorithmLZ4, level: 0, compressed: true},
		{name: "lz4 level9", algorithm: CompressionAlgorithmLZ4, level: 9, compressed: true},
		{name: "snappy", algorithm: CompressionAlgorithmSnappy, level: 0, compressed: true},
		{name: "zstd level1", algorithm: CompressionAlgorithmZstd, level: 1, compressed: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			codec, err := NewCompressCodec(tt.algorithm, 1, tt.level)
			require.NoError(t, err)

			encoded, flags, err := codec.Encode([]byte("key"), src, 0x1234)
			require.NoError(t, err)
			wantFlags := uint32(0xA0123400)
			if tt.compressed {
				wantFlags |= uint32(tt.algorithm) << 24
				assert.Less(t, len(encoded), len(src))
			} else {
				assert.Equal(t, src, encoded)
			}
			assert.Equal(t, wantFlags, flags)

			decoded, decodedFlags, err := codec.Decode([]byte("key"), encoded, flags)
			require.NoError(t, err)
			assert.Equal(t, src, decoded)
			assert.Equal(t, uint32(0x1234), decodedFlags)
		})
	}
}

func TestCompressionDeflateUsesZlibFormat(t *testing.T) {
	src := []byte("hello hello hello hello hello hello")
	compressed, err := compress(src, CompressionAlgorithmDeflate, 6)
	require.NoError(t, err)

	reader, err := zlib.NewReader(bytes.NewReader(compressed))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, reader.Close()) })

	decoded := bytes.Buffer{}
	_, err = decoded.ReadFrom(reader)
	require.NoError(t, err)
	assert.Equal(t, src, decoded.Bytes())
}

func TestCompressionDeflateDecodesPythonZlibFixture(t *testing.T) {
	src := []byte("hello hello hello hello hello hello")
	pythonZlib := []byte{120, 156, 203, 72, 205, 201, 201, 87, 200, 192, 71, 2, 0, 235, 85, 13, 25}

	decoded, err := decompress(pythonZlib, CompressionAlgorithmDeflate)
	require.NoError(t, err)
	assert.Equal(t, src, decoded)
}

func TestCompressionDeflateRejectsRawDeflate(t *testing.T) {
	src := []byte("hello hello hello hello hello hello")
	var buf bytes.Buffer
	writer, err := flate.NewWriter(&buf, flate.DefaultCompression)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, writer.Close()) })
	_, err = writer.Write(src)
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	decoded, err := decompress(buf.Bytes(), CompressionAlgorithmDeflate)
	assert.Nil(t, decoded)
	require.Error(t, err)
}

func TestDecompressedValueSizeLimit(t *testing.T) {
	assert.Equal(t, int64(100), decompressedValueSizeLimit(1))
	assert.Equal(t, int64(maxDecompressedValueSize), decompressedValueSizeLimit(maxDecompressedValueSize))
}

func TestDecompressRejectsOversizedPayload(t *testing.T) {
	src := bytes.Repeat([]byte("a"), 4096)
	var buf bytes.Buffer
	writer := zlib.NewWriter(&buf)
	t.Cleanup(func() { require.NoError(t, writer.Close()) })
	_, err := writer.Write(src)
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	require.Greater(t, len(src), len(buf.Bytes())*maxCompressionExpansionRatio)

	decoded, err := decompress(buf.Bytes(), CompressionAlgorithmDeflate)
	assert.Nil(t, decoded)
	require.Error(t, err)
	assert.ErrorIs(t, err, errInvalidValue)
}

func TestEncodeFallbackForSmallPayload(t *testing.T) {
	codec, err := NewCompressCodec(CompressionAlgorithmDeflate, defaultCompressionThreshold, 6)
	require.NoError(t, err)
	small := []byte("small payload")
	encodedValue, encodedFlags, err := codec.Encode([]byte("key"), small, 0x12)
	require.NoError(t, err)
	assert.Equal(t, small, encodedValue)
	assert.False(t, IsUnconventional(encodedFlags))
	assert.Equal(t, uint32(0x12), AppFlags(encodedFlags))
	assert.False(t, IsCompressed(encodedFlags))
}

func TestEncodeRejectsAppFlagsOutsideMCCompressRange(t *testing.T) {
	codec, err := NewCompressCodec(CompressionAlgorithmNone, 0, 6)
	require.NoError(t, err)
	encoded, flags, err := codec.Encode([]byte("key"), []byte("value"), 0x10000)
	require.ErrorIs(t, err, errInvalidFlags)
	assert.Nil(t, encoded)
	assert.Zero(t, flags)
}

func TestNewCompressCodecRejectsInvalidCompressionLevel(t *testing.T) {
	tests := []struct {
		name      string
		algorithm Compression
		level     int
	}{
		{name: "deflate below huffman only", algorithm: CompressionAlgorithmDeflate, level: flate.HuffmanOnly - 1},
		{name: "deflate above best compression", algorithm: CompressionAlgorithmDeflate, level: flate.BestCompression + 1},
		{name: "lz4 below fast", algorithm: CompressionAlgorithmLZ4, level: -1},
		{name: "lz4 above level9", algorithm: CompressionAlgorithmLZ4, level: 10},
		{name: "snappy rejects explicit level", algorithm: CompressionAlgorithmSnappy, level: 1},
		{name: "zstd below level1", algorithm: CompressionAlgorithmZstd, level: 0},
		{name: "zstd above level22", algorithm: CompressionAlgorithmZstd, level: 23},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			codec, err := NewCompressCodec(tt.algorithm, 1, tt.level)
			assert.Nil(t, codec)
			require.ErrorIs(t, err, errInvalidLevel)
		})
	}
}

func TestNewCompressCodecAcceptsMaximumZstdLevel(t *testing.T) {
	// Check the upper boundary without allocating a maximum-level encoder.
	codec, err := NewCompressCodec(CompressionAlgorithmZstd, 1, 22)
	require.NoError(t, err)
	require.NotNil(t, codec)
}

func TestDecodeRetrievedValueInvalidCompressionReturnsMiss(t *testing.T) {
	codec, err := NewCompressCodec(CompressionAlgorithmDeflate, 1, 6)
	require.NoError(t, err)

	for _, tt := range []struct {
		name  string
		flags uint32
	}{
		{name: "unknown algorithm", flags: 0xAF000000},
		{name: "invalid deflate payload", flags: 0xA1004400},
	} {
		t.Run(tt.name, func(t *testing.T) {
			decoded, decodedFlags, err := codec.Decode([]byte("key"), []byte("invalid payload"), tt.flags)
			require.ErrorIs(t, err, errNotFound)
			assert.Nil(t, decoded)
			assert.Zero(t, decodedFlags)
		})
	}
}

func TestDecodeRetrievedValueUncompressed(t *testing.T) {
	codec, err := NewCompressCodec(CompressionAlgorithmDeflate, 1, 6)
	require.NoError(t, err)
	value := []byte("plain")

	for _, tt := range []struct {
		name      string
		flags     uint32
		wantFlags uint32
	}{
		{name: "legacy flags", flags: 0x12345678, wantFlags: 0x12345678},
		{name: "MC-COMPRESS flags", flags: 0xA0004400, wantFlags: 0x44},
	} {
		t.Run(tt.name, func(t *testing.T) {
			decoded, decodedFlags, err := codec.Decode([]byte("key"), value, tt.flags)
			require.NoError(t, err)
			assert.Equal(t, value, decoded)
			assert.Equal(t, tt.wantFlags, decodedFlags)
		})
	}
}

func TestMCFlagsHelpers(t *testing.T) {
	tests := []struct {
		name               string
		flags              uint32
		wantUnconventional bool
		wantCompressed     bool
		wantAppFlags       uint32
	}{
		{name: "legacy flags", flags: 0x12345678, wantUnconventional: true, wantCompressed: false, wantAppFlags: 0x12345678},
		{name: "valid uncompressed", flags: 0xA0123400, wantUnconventional: false, wantCompressed: false, wantAppFlags: 0x1234},
		{name: "valid deflate", flags: 0xA100FF00, wantUnconventional: false, wantCompressed: true, wantAppFlags: 0x00FF},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.wantUnconventional, IsUnconventional(tt.flags))
			assert.Equal(t, tt.wantCompressed, IsCompressed(tt.flags))
			assert.Equal(t, tt.wantAppFlags, AppFlags(tt.flags))
		})
	}
}
