package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	memcached "github.com/yeqown/memcached"
)

// Unimplemented methods fail through the nil embedded Client.
type fakeMemcachedClient struct {
	memcached.Client
	metaGetCalled bool
	metaGetKey    string
}

func (f *fakeMemcachedClient) MetaGet(ctx context.Context, key []byte, options ...memcached.MetaGetOption) (*memcached.MetaItem, error) {
	f.metaGetCalled = true
	f.metaGetKey = string(key)
	return &memcached.MetaItem{
		Key:   key,
		Value: []byte(`{"name":"meta-value"}`),
	}, nil
}

var _ memcached.Client = (*fakeMemcachedClient)(nil)

func TestOperationServiceNormalizeMemcachedKey(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		expected  string
		expectErr string
	}{
		{name: "trim spaces", input: "  sample-key  ", expected: "sample-key"},
		{name: "empty", input: "   ", expectErr: "key cannot be empty"},
		{name: "contains spaces", input: "l: conf:game:101", expectErr: "key cannot contain whitespace"},
		{name: "contains tab", input: "foo\tbar", expectErr: "key cannot contain whitespace"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := normalizeMemcachedKey(tt.input)
			if tt.expectErr != "" {
				require.Error(t, err)
				require.Equal(t, tt.expectErr, err.Error())
				require.Empty(t, got)
				return
			}

			require.NoError(t, err)
			require.Equal(t, tt.expected, got)
		})
	}
}

func TestOperationServiceGetTrimsKeyBeforeMetaGet(t *testing.T) {
	fakeClient := &fakeMemcachedClient{}
	conn := &ConnectionService{client: fakeClient}
	svc := NewOperationService(conn)

	result := svc.Get("  sample-key  ")
	require.True(t, result.Success)
	require.Equal(t, "sample-key", fakeClient.metaGetKey)
}

func TestOperationServiceGetRejectsWhitespaceKey(t *testing.T) {
	fakeClient := &fakeMemcachedClient{}
	conn := &ConnectionService{client: fakeClient}
	svc := NewOperationService(conn)

	result := svc.Get("l: conf:game:101")
	require.False(t, result.Success)
	require.Equal(t, "key cannot contain whitespace", result.Error)
	require.False(t, fakeClient.metaGetCalled)
}
