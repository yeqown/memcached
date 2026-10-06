package telemetry

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/metric/noop"
)

func TestMetricsRecordResolve(t *testing.T) {
	meter := &capturingMeter{}
	m, err := newMetrics(capturingProvider{meter: meter})
	require.NoError(t, err)
	ctx := context.Background()
	before := time.Now().Unix()
	m.RecordResolve(ctx, 250*time.Millisecond, nil)
	m.RecordResolve(ctx, 500*time.Millisecond, errors.New("discovery unavailable"))
	after := time.Now().Unix()

	require.Equal(t, []float64{0.25, 0.5}, meter.values("memcached.discovery.resolve.duration"))
	require.Equal(t, []float64{1, 1}, meter.values("memcached.discovery.resolve.calls"))
	require.Equal(t, []float64{1}, meter.values("memcached.discovery.resolve.errors"))
	success := meter.values("memcached.discovery.resolve.last_success")
	require.Len(t, success, 1, "a failed attempt must not overwrite last success")
	require.GreaterOrEqual(t, success[0], float64(before))
	require.LessOrEqual(t, success[0], float64(after))
	assertClientAttributes(t, meter.measurements)
}

func TestMetricsRecordTopology(t *testing.T) {
	meter := &capturingMeter{}
	m, err := newMetrics(capturingProvider{meter: meter})
	require.NoError(t, err)
	ctx := context.Background()
	m.RecordTopology(ctx, 3, 1)
	m.RecordTopology(ctx, 4, 2)
	m.RecordTopology(ctx, 4, 2)
	m.RecordTopology(ctx, 0, 2)

	require.Equal(t, []float64{3, 4, 4, 0}, meter.values("memcached.topology.nodes"))
	require.Equal(t, []float64{1, 2, 2, 2}, meter.values("memcached.topology.generation"))
	assertClientAttributes(t, meter.measurements)
}

func TestMetricsSharedProviderKeepsClientTopologiesSeparate(t *testing.T) {
	meter := &capturingMeter{}
	provider := capturingProvider{meter: meter}
	first, err := newMetrics(provider)
	require.NoError(t, err)
	second, err := newMetrics(provider)
	require.NoError(t, err)
	ctx := context.Background()
	first.RecordTopology(ctx, 2, 1)
	second.RecordTopology(ctx, 5, 3)
	first.RecordTopology(ctx, 0, 1)

	byClient := make(map[string]float64)
	for _, measurement := range meter.measurements {
		if measurement.name == "memcached.topology.nodes" {
			id, ok := measurement.attributes.Value("memcached.client.id")
			require.True(t, ok)
			byClient[id.AsString()] = measurement.value
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
			meter := &capturingMeter{}
			m, err := newMetrics(capturingProvider{meter: meter})
			require.NoError(t, err)
			m.RecordTopology(t.Context(), 1, test.generation)
			require.Equal(t, []float64{float64(test.generation)}, meter.values("memcached.topology.generation"))
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
			meter := &capturingMeter{failName: name, failure: failure}
			m, err := newMetrics(capturingProvider{meter: meter})
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

func TestMetricsOperationMeasurementsPreserved(t *testing.T) {
	meter := &capturingMeter{}
	m, err := newMetrics(capturingProvider{meter: meter})
	require.NoError(t, err)
	m.RecordDuration(context.Background(), "get", "cache.example:11211", 100*time.Millisecond, errors.New("cache miss"))
	require.Equal(t, []float64{0.1}, meter.values("memcached.operation.duration"))
	require.Equal(t, []float64{1}, meter.values("memcached.operation.calls"))
	require.Equal(t, []float64{1}, meter.values("memcached.operation.errors"))
	for _, measurement := range meter.measurements {
		require.Equal(t, attribute.NewSet(
			attribute.String("db.system", "memcached"),
			attribute.String("db.operation", "get"),
			attribute.String("net.peer.name", "cache.example:11211"),
		), measurement.attributes)
	}
}

func TestMetricsConcurrentRecording(t *testing.T) {
	meter := &capturingMeter{}
	m, err := newMetrics(capturingProvider{meter: meter})
	require.NoError(t, err)
	var workers sync.WaitGroup
	for range 20 {
		workers.Go(func() {
			m.RecordResolve(context.Background(), time.Millisecond, nil)
			m.RecordTopology(context.Background(), 2, 1)
		})
	}
	workers.Wait()
	require.Len(t, meter.values("memcached.discovery.resolve.calls"), 20)
	require.Len(t, meter.values("memcached.topology.nodes"), 20)
	assertClientAttributes(t, meter.measurements)
}

func assertClientAttributes(t *testing.T, measurements []capturedMeasurement) {
	t.Helper()
	var identity string
	for _, measurement := range measurements {
		require.Equal(t, 1, measurement.attributes.Len(), "discovery labels must not expose the target or topology")
		id, ok := measurement.attributes.Value("memcached.client.id")
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

// Capturing instruments record the measurements emitted at the OTel API boundary
// without adding an SDK dependency or imposing SDK aggregation behavior.
type capturedMeasurement struct {
	name       string
	value      float64
	attributes attribute.Set
}

type capturingProvider struct {
	noop.MeterProvider
	meter *capturingMeter
}

func (p capturingProvider) Meter(string, ...metric.MeterOption) metric.Meter {
	return p.meter
}

type capturingMeter struct {
	noop.Meter
	mu           sync.Mutex
	measurements []capturedMeasurement
	failName     string
	failure      error
}

func (m *capturingMeter) Int64Counter(name string, _ ...metric.Int64CounterOption) (metric.Int64Counter, error) {
	if name == m.failName {
		return nil, m.failure
	}
	return capturingCounter{meter: m, name: name}, nil
}

func (m *capturingMeter) Float64Histogram(name string, _ ...metric.Float64HistogramOption) (metric.Float64Histogram, error) {
	if name == m.failName {
		return nil, m.failure
	}
	return capturingHistogram{meter: m, name: name}, nil
}

func (m *capturingMeter) Int64Gauge(name string, _ ...metric.Int64GaugeOption) (metric.Int64Gauge, error) {
	if name == m.failName {
		return nil, m.failure
	}
	return capturingGauge{meter: m, name: name}, nil
}

func (m *capturingMeter) Float64Gauge(name string, _ ...metric.Float64GaugeOption) (metric.Float64Gauge, error) {
	if name == m.failName {
		return nil, m.failure
	}
	return capturingFloatGauge{meter: m, name: name}, nil
}

func (m *capturingMeter) capture(name string, value float64, attributes attribute.Set) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.measurements = append(m.measurements, capturedMeasurement{name: name, value: value, attributes: attributes})
}

func (m *capturingMeter) values(name string) []float64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	var values []float64
	for _, measurement := range m.measurements {
		if measurement.name == name {
			values = append(values, measurement.value)
		}
	}
	return values
}

type capturingCounter struct {
	noop.Int64Counter
	meter *capturingMeter
	name  string
}

func (c capturingCounter) Add(_ context.Context, value int64, options ...metric.AddOption) {
	config := metric.NewAddConfig(options)
	c.meter.capture(c.name, float64(value), config.Attributes())
}

type capturingHistogram struct {
	noop.Float64Histogram
	meter *capturingMeter
	name  string
}

func (h capturingHistogram) Record(_ context.Context, value float64, options ...metric.RecordOption) {
	config := metric.NewRecordConfig(options)
	h.meter.capture(h.name, value, config.Attributes())
}

type capturingGauge struct {
	noop.Int64Gauge
	meter *capturingMeter
	name  string
}

func (g capturingGauge) Record(_ context.Context, value int64, options ...metric.RecordOption) {
	config := metric.NewRecordConfig(options)
	g.meter.capture(g.name, float64(value), config.Attributes())
}

type capturingFloatGauge struct {
	noop.Float64Gauge
	meter *capturingMeter
	name  string
}

func (g capturingFloatGauge) Record(_ context.Context, value float64, options ...metric.RecordOption) {
	config := metric.NewRecordConfig(options)
	g.meter.capture(g.name, value, config.Attributes())
}
