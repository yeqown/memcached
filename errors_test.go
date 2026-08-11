package memcached

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWrappedErrorChainPreservesSentinel(t *testing.T) {
	err := fmt.Errorf("request failed: %w", fmt.Errorf("send failed: %w", ErrNotFound))
	assert.True(t, errors.Is(err, ErrNotFound))
	assert.Contains(t, err.Error(), "request failed")
	assert.Contains(t, err.Error(), "send failed")
}

func TestErrorsJoinPreservesJoinedSentinels(t *testing.T) {
	joined := errors.Join(
		fmt.Errorf("node-a: %w", ErrNotFound),
		fmt.Errorf("node-b: %w", ErrServerError),
	)
	require.Error(t, joined)
	assert.True(t, errors.Is(joined, ErrNotFound))
	assert.True(t, errors.Is(joined, ErrServerError))
}
