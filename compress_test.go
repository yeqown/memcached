package memcached

import (
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

	decoded, err := tryDecompressValue(compressed, flags)
	require.NoError(t, err)
	assert.Equal(t, src, decoded)
}

func TestDecodeRetrievedValueUnknownAlgorithmReturnsMiss(t *testing.T) {
	flags := MCFlags(0xAF000000)
	decoded, err := tryDecompressValue([]byte("payload"), flags)
	assert.Nil(t, decoded)
	require.Error(t, err)
	assert.True(t, pkgerrors.Is(err, ErrNotFound))
}

func TestDecodeRetrievedValueInvalidPayloadReturnsMiss(t *testing.T) {
	flags, err := buildMCFlags(0x44, CompressionAlgorithmDeflate)
	require.NoError(t, err)

	decoded, err := tryDecompressValue([]byte("not-deflate"), flags)
	assert.Nil(t, decoded)
	require.Error(t, err)
	assert.True(t, pkgerrors.Is(err, ErrNotFound))
}

func TestDecodeRetrievedValueNonMCFlags(t *testing.T) {
	value := []byte("plain")
	decoded, err := tryDecompressValue(value, MCFlags(123))
	require.NoError(t, err)
	assert.Equal(t, value, decoded)
}
