package telemetry_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/yeqown/memcached/telemetry"
)

func TestNilTracerStartPreservesContext(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var tracer *telemetry.Tracer

	require.NotPanics(t, func() {
		got, span := tracer.Start(ctx, "get", "cache.example:11211", "tcp", "key")
		require.Same(t, ctx, got)
		require.Nil(t, span)
	})
}

func TestNilTelemetryFinish(t *testing.T) {
	for _, test := range []struct {
		name string
		err  error
	}{
		{name: "success"},
		{name: "failure", err: errors.New("request failed")},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Run("tracer", func(t *testing.T) {
				var tracer *telemetry.Tracer
				require.NotPanics(t, func() { tracer.End(nil, test.err) })
			})
			t.Run("metrics", func(t *testing.T) {
				var metrics *telemetry.Metrics
				require.NotPanics(t, func() {
					metrics.RecordDuration(t.Context(), "get", "cache.example:11211", time.Millisecond, test.err)
				})
			})
		})
	}
}
