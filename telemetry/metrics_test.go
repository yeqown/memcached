package telemetry

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/yeqown/memcached/internal/testutil"
	"go.opentelemetry.io/otel/attribute"
)

func TestMetricsRecordResolve(t *testing.T) {
	meter := &testutil.CaptureMeter{}
	m, err := newMetrics(meter.Provider())
	require.NoError(t, err)
	ctx := context.Background()
	before := time.Now().Unix()
	m.RecordResolve(ctx, 250*time.Millisecond, nil)
	m.RecordResolve(ctx, 500*time.Millisecond, errors.New("discovery unavailable"))
	after := time.Now().Unix()

	require.Equal(t, []float64{0.25, 0.5}, meter.Values("memcached.discovery.resolve.duration"))
	require.Equal(t, []float64{1, 1}, meter.Values("memcached.discovery.resolve.calls"))
	require.Equal(t, []float64{1}, meter.Values("memcached.discovery.resolve.errors"))
	success := meter.Values("memcached.discovery.resolve.last_success")
	require.Len(t, success, 1, "a failed attempt must not overwrite last success")
	require.GreaterOrEqual(t, success[0], float64(before))
	require.LessOrEqual(t, success[0], float64(after))
	assertClientAttributes(t, meter.Measurements())
}

func TestMetricsRecordTopology(t *testing.T) {
	meter := &testutil.CaptureMeter{}
	m, err := newMetrics(meter.Provider())
	require.NoError(t, err)
	ctx := context.Background()
	m.RecordTopology(ctx, 3, 1)
	m.RecordTopology(ctx, 4, 2)
	m.RecordTopology(ctx, 4, 2)
	m.RecordTopology(ctx, 0, 2)

	require.Equal(t, []float64{3, 4, 4, 0}, meter.Values("memcached.topology.nodes"))
	require.Equal(t, []float64{1, 2, 2, 2}, meter.Values("memcached.topology.generation"))
	assertClientAttributes(t, meter.Measurements())
}

func TestMetricsSharedProviderKeepsClientTopologiesSeparate(t *testing.T) {
	meter := &testutil.CaptureMeter{}
	provider := meter.Provider()
	first, err := newMetrics(provider)
	require.NoError(t, err)
	second, err := newMetrics(provider)
	require.NoError(t, err)
	ctx := context.Background()
	first.RecordTopology(ctx, 2, 1)
	second.RecordTopology(ctx, 5, 3)
	first.RecordTopology(ctx, 0, 1)

	byClient := make(map[string]float64)
	for _, measurement := range meter.Measurements() {
		if measurement.Name == "memcached.topology.nodes" {
			id, ok := measurement.Attributes.Value("memcached.client.id")
			require.True(t, ok)
			byClient[id.AsString()] = measurement.Value
		}
	}
	require.Len(t, byClient, 2, "closing one client must not replace another client's gauge")
	require.ElementsMatch(t, []float64{0, 5}, mapValues(byClient))
}

func TestMetricsRecordTopologyUnsignedGeneration(t *testing.T) {
	for _, test := range []struct {
		name       string
		generation uint64
	}{
		{name: "zero"},
		{name: "above signed range", generation: 1 << 63},
		{name: "maximum uint64", generation: 1<<64 - 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			meter := &testutil.CaptureMeter{}
			m, err := newMetrics(meter.Provider())
			require.NoError(t, err)
			m.RecordTopology(t.Context(), 1, test.generation)
			require.Equal(t, []float64{float64(test.generation)}, meter.Values("memcached.topology.generation"))
		})
	}
}

func TestMetricsRegistrationFailures(t *testing.T) {
	names := []string{
		"memcached.operation.duration",
		"memcached.operation.calls",
		"memcached.operation.errors",
		"memcached.discovery.resolve.duration",
		"memcached.discovery.resolve.calls",
		"memcached.discovery.resolve.errors",
		"memcached.discovery.resolve.last_success",
		"memcached.topology.nodes",
		"memcached.topology.generation",
	}
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			failure := errors.New("instrument registration failed")
			meter := &testutil.CaptureMeter{FailName: name, Failure: failure}
			m, err := newMetrics(meter.Provider())
			require.ErrorIs(t, err, failure)
			require.Nil(t, m)
		})
	}
}

func TestMetricsDisabledWithoutProvider(t *testing.T) {
	require.Nil(t, NewConfig().Metrics())
	require.Nil(t, NewConfig(WithMeterProvider(nil)).Metrics())
	var config *Config
	require.Nil(t, config.Metrics())
}

func TestMetricsRecordDuration(t *testing.T) {
	meter := &testutil.CaptureMeter{}
	m, err := newMetrics(meter.Provider())
	require.NoError(t, err)
	m.RecordDuration(t.Context(), "get", "cache.example:11211", 100*time.Millisecond, nil)
	m.RecordDuration(t.Context(), "get", "cache.example:11211", 200*time.Millisecond, errors.New("cache miss"))
	require.Equal(t, []float64{0.1, 0.2}, meter.Values("memcached.operation.duration"))
	require.Equal(t, []float64{1, 1}, meter.Values("memcached.operation.calls"))
	require.Equal(t, []float64{1}, meter.Values("memcached.operation.errors"))
	for _, measurement := range meter.Measurements() {
		require.Equal(t, attribute.NewSet(
			attribute.String("db.system", "memcached"),
			attribute.String("db.operation", "get"),
			attribute.String("net.peer.name", "cache.example:11211"),
		), measurement.Attributes)
	}
}

func assertClientAttributes(t *testing.T, measurements []testutil.Measurement) {
	t.Helper()
	var identity string
	for _, measurement := range measurements {
		require.Equal(t, 1, measurement.Attributes.Len(), "discovery labels must not expose the target or topology")
		id, ok := measurement.Attributes.Value("memcached.client.id")
		require.True(t, ok)
		require.NotEmpty(t, id.AsString())
		if identity == "" {
			identity = id.AsString()
		}
		require.Equal(t, identity, id.AsString(), "the client identity must stay stable")
	}
}

func mapValues(values map[string]float64) []float64 {
	result := make([]float64, 0, len(values))
	for _, value := range values {
		result = append(result, value)
	}
	return result
}
