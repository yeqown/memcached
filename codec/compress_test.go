package codec

import (
	"bytes"
	"compress/flate"
	"compress/zlib"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCompressionRoundTripDeflate(t *testing.T) {
	src := []byte("hello hello hello hello hello hello")
	compressed, err := compress(src, CompressionAlgorithmDeflate, 6)
	require.NoError(t, err)
	require.NotEqual(t, src, compressed)

	decoded, err := decompress(compressed, CompressionAlgorithmDeflate)
	require.NoError(t, err)
	assert.Equal(t, src, decoded)
}

func TestCompressionRoundTripAlgorithms(t *testing.T) {
	src := bytes.Repeat([]byte("hello hello hello hello hello hello random-ish padding "), 8)

	tests := []struct {
		name      string
		algorithm Compression
	}{
		{name: "lz4", algorithm: CompressionAlgorithmLZ4},
		{name: "snappy", algorithm: CompressionAlgorithmSnappy},
		{name: "zstd", algorithm: CompressionAlgorithmZstd},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compressed, err := compress(src, tt.algorithm, 6)
			require.NoError(t, err)
			require.NotEqual(t, src, compressed)

			decoded, err := decompress(compressed, tt.algorithm)
			require.NoError(t, err)
			assert.Equal(t, src, decoded)
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
	_, _, err = codec.Encode([]byte("key"), []byte("value"), 0x10000)
	require.Error(t, err)
}

func TestNewCompressCodecStoresCompressionLevel(t *testing.T) {
	codec, err := NewCompressCodec(CompressionAlgorithmDeflate, 1, flate.BestCompression)
	require.NoError(t, err)
	compressCodec := codec.(*compressCodec)
	assert.Equal(t, flate.BestCompression, compressCodec.compressionLevel)
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
			require.Error(t, err)
		})
	}
}

func TestCompressDoesNotValidateCompressionLevel(t *testing.T) {
	compressed, err := compress([]byte("hello hello hello hello"), CompressionAlgorithmSnappy, 1)
	require.NoError(t, err)
	require.NotEmpty(t, compressed)
}

func TestNewCompressCodecAcceptsSupportedCompressionLevels(t *testing.T) {
	tests := []struct {
		name      string
		algorithm Compression
		level     int
	}{
		{name: "none zero", algorithm: CompressionAlgorithmNone, level: 0},
		{name: "deflate huffman only", algorithm: CompressionAlgorithmDeflate, level: flate.HuffmanOnly},
		{name: "deflate default", algorithm: CompressionAlgorithmDeflate, level: flate.DefaultCompression},
		{name: "deflate best speed", algorithm: CompressionAlgorithmDeflate, level: flate.BestSpeed},
		{name: "deflate best compression", algorithm: CompressionAlgorithmDeflate, level: flate.BestCompression},
		{name: "lz4 fast", algorithm: CompressionAlgorithmLZ4, level: 0},
		{name: "lz4 level9", algorithm: CompressionAlgorithmLZ4, level: 9},
		{name: "snappy zero", algorithm: CompressionAlgorithmSnappy, level: 0},
		{name: "zstd level1", algorithm: CompressionAlgorithmZstd, level: 1},
		{name: "zstd level22", algorithm: CompressionAlgorithmZstd, level: 22},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			codec, err := NewCompressCodec(tt.algorithm, 1, tt.level)
			require.NoError(t, err)
			require.NotNil(t, codec)
		})
	}
}

func TestDecodeRetrievedValueCompressed(t *testing.T) {
	codec, err := NewCompressCodec(CompressionAlgorithmDeflate, 1, 6)
	require.NoError(t, err)
	src := []byte("hello hello hello hello hello hello")
	compressed, err := compress(src, CompressionAlgorithmDeflate, 6)
	require.NoError(t, err)
	flags := newCompressFlag(0x44, CompressionAlgorithmDeflate)

	decoded, decodedFlags, err := codec.Decode([]byte("key"), compressed, uint32(flags))
	require.NoError(t, err)
	assert.Equal(t, src, decoded)
	assert.Equal(t, uint32(0x44), decodedFlags)
}

func TestDecodeRetrievedValueUnknownAlgorithmReturnsMiss(t *testing.T) {
	codec, err := NewCompressCodec(CompressionAlgorithmDeflate, 1, 6)
	require.NoError(t, err)
	flags := uint32(0xAF000000)
	decoded, decodedFlags, err := codec.Decode([]byte("key"), []byte("payload"), flags)
	assert.Nil(t, decoded)
	assert.Zero(t, decodedFlags)
	require.Error(t, err)
	assert.ErrorIs(t, err, errNotFound)
}

func TestDecodeRetrievedValueInvalidPayloadReturnsMiss(t *testing.T) {
	codec, err := NewCompressCodec(CompressionAlgorithmDeflate, 1, 6)
	require.NoError(t, err)
	flags := newCompressFlag(0x44, CompressionAlgorithmDeflate)

	decoded, decodedFlags, err := codec.Decode([]byte("key"), []byte("not-deflate"), uint32(flags))
	assert.Nil(t, decoded)
	assert.Zero(t, decodedFlags)
	require.Error(t, err)
	assert.ErrorIs(t, err, errNotFound)
}

func TestDecodeRetrievedValueNonMCFlags(t *testing.T) {
	codec, err := NewCompressCodec(CompressionAlgorithmDeflate, 1, 6)
	require.NoError(t, err)
	value := []byte("plain")
	decoded, decodedFlags, err := codec.Decode([]byte("key"), value, 123)
	require.NoError(t, err)
	assert.Equal(t, value, decoded)
	assert.Equal(t, uint32(123), decodedFlags)
}

func TestDecodeRetrievedValueConventionalUncompressedFlags(t *testing.T) {
	codec, err := NewCompressCodec(CompressionAlgorithmDeflate, 1, 6)
	require.NoError(t, err)
	value := []byte("plain")
	flags := uint32(newCompressFlag(0x44, CompressionAlgorithmNone))

	decoded, decodedFlags, err := codec.Decode([]byte("key"), value, flags)
	require.NoError(t, err)
	assert.Equal(t, value, decoded)
	assert.Equal(t, uint32(0x44), decodedFlags)
}

func TestMCFlagsHelpers(t *testing.T) {
	uncompressed := newCompressFlag(0x1234, CompressionAlgorithmNone)
	deflate := newCompressFlag(0x00FF, CompressionAlgorithmDeflate)

	tests := []struct {
		name               string
		flags              uint32
		wantUnconventional bool
		wantCompressed     bool
		wantAppFlags       uint32
	}{
		{name: "legacy flags", flags: 0x12345678, wantUnconventional: true, wantCompressed: false, wantAppFlags: 0x12345678},
		{name: "valid uncompressed", flags: uint32(uncompressed), wantUnconventional: false, wantCompressed: false, wantAppFlags: 0x1234},
		{name: "valid deflate", flags: uint32(deflate), wantUnconventional: false, wantCompressed: true, wantAppFlags: 0x00FF},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.wantUnconventional, IsUnconventional(tt.flags))
			assert.Equal(t, tt.wantCompressed, IsCompressed(tt.flags))
			assert.Equal(t, tt.wantAppFlags, AppFlags(tt.flags))
		})
	}
}

func TestCompressFlag(t *testing.T) {
	uncompressed := newCompressFlag(0x1234, CompressionAlgorithmNone)
	deflate := newCompressFlag(0x00FF, CompressionAlgorithmDeflate)
	legacy := compressFlag(0x12345678)

	tests := []struct {
		name               string
		flag               compressFlag
		wantRaw            uint32
		wantUnconventional bool
		wantCompressed     bool
		wantAppFlags       uint32
		wantAlgorithm      Compression
	}{
		{
			name:               "legacy flags",
			flag:               legacy,
			wantRaw:            0x12345678,
			wantUnconventional: true,
			wantCompressed:     false,
			wantAppFlags:       0x12345678,
			wantAlgorithm:      CompressionAlgorithmNone,
		},
		{
			name:               "valid uncompressed",
			flag:               uncompressed,
			wantRaw:            0xA0123400,
			wantUnconventional: false,
			wantCompressed:     false,
			wantAppFlags:       0x1234,
			wantAlgorithm:      CompressionAlgorithmNone,
		},
		{
			name:               "valid deflate",
			flag:               deflate,
			wantRaw:            0xA100FF00,
			wantUnconventional: false,
			wantCompressed:     true,
			wantAppFlags:       0x00FF,
			wantAlgorithm:      CompressionAlgorithmDeflate,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.wantRaw, uint32(tt.flag))
			assert.Equal(t, tt.wantUnconventional, tt.flag.unconventional())
			assert.Equal(t, tt.wantCompressed, tt.flag.isCompressed())
			assert.Equal(t, tt.wantAppFlags, tt.flag.appFlags())
			assert.Equal(t, tt.wantAlgorithm, tt.flag.compressionAlgorithm())
		})
	}
}
