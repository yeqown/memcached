package memcached

import (
	"bytes"
	"compress/flate"
	"compress/zlib"
	"testing"

	pkgerrors "github.com/pkg/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCompressionRoundTripDeflate(t *testing.T) {
	src := []byte("hello hello hello hello hello hello")
	compressed, err := compress(src, CompressionAlgorithmDeflate)
	require.NoError(t, err)
	require.NotEqual(t, src, compressed)

	decoded, err := decompress(compressed, CompressionAlgorithmDeflate)
	require.NoError(t, err)
	assert.Equal(t, src, decoded)
}

func TestCompressionDeflateUsesZlibFormat(t *testing.T) {
	src := []byte("hello hello hello hello hello hello")
	compressed, err := compress(src, CompressionAlgorithmDeflate)
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
	assert.True(t, pkgerrors.Is(err, ErrInvalidValue))
}

func TestPrepareStorageValueFallbackAndEncode(t *testing.T) {
	small := []byte("small payload")
	preparedValue, preparedFlags, err := prepareStorageValue(small, 0x12, CompressionAlgorithmDeflate, defaultCompressionThreshold)
	require.NoError(t, err)
	assert.Equal(t, small, preparedValue)
	assert.False(t, preparedFlags.unconventional())
	assert.Equal(t, CompressionAlgorithmNone, preparedFlags.compressionAlgorithm())
	assert.Equal(t, uint32(0x12), preparedFlags.AppFlags())
}

func TestDecodeRetrievedValueCompressed(t *testing.T) {
	src := []byte("hello hello hello hello hello hello")
	compressed, err := compress(src, CompressionAlgorithmDeflate)
	require.NoError(t, err)
	flags, err := buildMCFlags(0x44, CompressionAlgorithmDeflate)
	require.NoError(t, err)

	decoded, err := tryDecompressValue(compressed, flags, "key")
	require.NoError(t, err)
	assert.Equal(t, src, decoded)
}

func TestDecodeRetrievedValueUnknownAlgorithmReturnsMiss(t *testing.T) {
	flags := MCFlags(0xAF000000)
	decoded, err := tryDecompressValue([]byte("payload"), flags, "key")
	assert.Nil(t, decoded)
	require.Error(t, err)
	assert.True(t, pkgerrors.Is(err, ErrNotFound))
}

func TestDecodeRetrievedValueInvalidPayloadReturnsMiss(t *testing.T) {
	flags, err := buildMCFlags(0x44, CompressionAlgorithmDeflate)
	require.NoError(t, err)

	decoded, err := tryDecompressValue([]byte("not-deflate"), flags, "key")
	assert.Nil(t, decoded)
	require.Error(t, err)
	assert.True(t, pkgerrors.Is(err, ErrNotFound))
}

func TestDecodeRetrievedValueNonMCFlags(t *testing.T) {
	value := []byte("plain")
	decoded, err := tryDecompressValue(value, MCFlags(123), "key")
	require.NoError(t, err)
	assert.Equal(t, value, decoded)
}
