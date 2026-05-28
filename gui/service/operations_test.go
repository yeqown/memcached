package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	memcached "github.com/yeqown/memcached"
)

type structuredOperationResult struct {
	Success          bool   `json:"success"`
	Data             string `json:"data"`
	Error            string `json:"error"`
	Key              string `json:"key"`
	Value            string `json:"value"`
	TTL              int64  `json:"ttl"`
	LastAccessedTime int64  `json:"lastAccessedTime"`
	CAS              uint64 `json:"cas"`
	Flags            uint16 `json:"flags"`
	Size             uint64 `json:"size"`
	HitBefore        bool   `json:"hitBefore"`
	Opaque           uint64 `json:"opaque"`
	ValueKind        string `json:"valueKind"`
}

type fakeMemcachedClient struct {
	getCalled     bool
	metaGetCalled bool
	metaGetKey    string
}

func (f *fakeMemcachedClient) Close() error { return nil }

func (f *fakeMemcachedClient) Set(context.Context, string, []byte, uint16, time.Duration) error {
	return nil
}

func (f *fakeMemcachedClient) Add(context.Context, string, []byte, uint16, time.Duration) error {
	return nil
}

func (f *fakeMemcachedClient) Replace(context.Context, string, []byte, uint16, time.Duration) error {
	return nil
}

func (f *fakeMemcachedClient) Append(context.Context, string, []byte, uint16, time.Duration) error {
	return nil
}

func (f *fakeMemcachedClient) Prepend(context.Context, string, []byte, uint16, time.Duration) error {
	return nil
}

func (f *fakeMemcachedClient) Cas(context.Context, string, []byte, uint16, time.Duration, uint64) error {
	return nil
}

func (f *fakeMemcachedClient) Get(ctx context.Context, key string) (*memcached.Item, error) {
	f.getCalled = true
	return &memcached.Item{Key: key, Value: []byte("plain-value")}, nil
}

func (f *fakeMemcachedClient) Gets(context.Context, ...string) ([]*memcached.Item, error) {
	return nil, nil
}

func (f *fakeMemcachedClient) GetAndTouch(context.Context, time.Duration, string) (*memcached.Item, error) {
	return nil, nil
}

func (f *fakeMemcachedClient) GetAndTouches(context.Context, time.Duration, ...string) ([]*memcached.Item, error) {
	return nil, nil
}

func (f *fakeMemcachedClient) Delete(context.Context, string) error { return nil }

func (f *fakeMemcachedClient) Incr(context.Context, string, uint64) (uint64, error) { return 0, nil }

func (f *fakeMemcachedClient) Decr(context.Context, string, uint64) (uint64, error) { return 0, nil }

func (f *fakeMemcachedClient) Touch(context.Context, string, time.Duration) error { return nil }

func (f *fakeMemcachedClient) Version(context.Context) (string, error) { return "", nil }

func (f *fakeMemcachedClient) FlushAll(context.Context) error { return nil }

func (f *fakeMemcachedClient) MetaSet(context.Context, []byte, []byte, ...memcached.MetaSetOption) (*memcached.MetaItem, error) {
	return nil, nil
}

func (f *fakeMemcachedClient) MetaGet(ctx context.Context, key []byte, options ...memcached.MetaGetOption) (*memcached.MetaItem, error) {
	f.metaGetCalled = true
	f.metaGetKey = string(key)
	return &memcached.MetaItem{
		Key:              key,
		Value:            []byte(`{"name":"meta-value"}`),
		TTL:              120,
		LastAccessedTime: 42,
		CAS:              99,
		Flags:            7,
		Size:             17,
		Opaque:           13,
		HitBefore:        true,
	}, nil
}

func (f *fakeMemcachedClient) MetaDelete(context.Context, []byte, ...memcached.MetaDeleteOption) (*memcached.MetaItem, error) {
	return nil, nil
}

func (f *fakeMemcachedClient) MetaArithmetic(context.Context, []byte, uint64, ...memcached.MetaArithmeticOption) (*memcached.MetaItem, error) {
	return nil, nil
}

func (f *fakeMemcachedClient) MetaDebug(context.Context, []byte, ...memcached.MetaDebugOption) (*memcached.MetaItemDebug, error) {
	return nil, nil
}

func (f *fakeMemcachedClient) MetaNoOp(context.Context) error { return nil }

func (f *fakeMemcachedClient) Stats(context.Context) (*memcached.Statistic, error) { return nil, nil }

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
