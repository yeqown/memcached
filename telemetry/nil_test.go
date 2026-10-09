package telemetry_test

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/yeqown/memcached/telemetry"
)

func TestNilTelemetry(t *testing.T) {
	ctx := t.Context()
	var tracer *telemetry.Tracer
	got, span := tracer.Start(ctx, "get", "cache.example:11211", "tcp", "key")
	require.Same(t, ctx, got)
	require.Nil(t, span)

	failure := errors.New("request failed")
	tracer.End(span, failure)
	var metrics *telemetry.Metrics
	metrics.RecordDuration(ctx, "get", "cache.example:11211", time.Millisecond, failure)
	metrics.RecordResolve(ctx, time.Millisecond, failure)
	metrics.RecordTopology(ctx, 1, 1)
}
