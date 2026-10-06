package telemetry_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	memresolver "github.com/yeqown/memcached/resolver"

	"github.com/stretchr/testify/require"
	"github.com/yeqown/memcached"
	"github.com/yeqown/memcached/telemetry"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/metric/noop"
)

func TestClientEmitsDiscoveryMetrics(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		meter := &clientMetricsMeter{}
		calls := 0
		resolver := metricsResolver(func(context.Context, string) (memresolver.ResolveResult, *time.Time, error) {
			calls++
			// Fake time makes resolve duration observable without wall-clock waits.
			time.Sleep(100 * time.Millisecond)
			next := time.Now().Add(time.Hour)
			switch calls {
			case 1:
				return memresolver.ResolveResult{Generation: 1, Addrs: []*memresolver.Addr{
					memresolver.NewAddr("tcp", "a.example:11211", 0),
					memresolver.NewAddr("tcp", "b.example:11211", 0),
				}}, &next, nil
			case 2:
				return memresolver.ResolveResult{}, &next, errors.New("directory temporarily unavailable")
			default:
				return memresolver.ResolveResult{Generation: 2, Addrs: []*memresolver.Addr{
					memresolver.NewAddr("tcp", "b.example:11211", 0),
					memresolver.NewAddr("tcp", "c.example:11211", 0),
					memresolver.NewAddr("tcp", "d.example:11211", 0),
				}}, nil, nil
			}
		})
		c, err := memcached.New("http://directory.example/nodes",
			memcached.WithResolver(resolver),
			memcached.WithTelemetry(telemetry.WithMeterProvider(clientMetricsProvider{meter: meter})),
		)
		require.NoError(t, err)
		defer func() { require.NoError(t, c.Close()) }()
		synctest.Wait()

		require.Equal(t, []float64{1}, meter.values("memcached.discovery.resolve.calls"))
		require.Empty(t, meter.values("memcached.discovery.resolve.errors"))
		firstSuccess := float64(time.Now().Unix())
		require.Equal(t, []float64{firstSuccess}, meter.values("memcached.discovery.resolve.last_success"))
		require.Equal(t, []float64{2}, meter.values("memcached.topology.nodes"))
		require.Equal(t, []float64{1}, meter.values("memcached.topology.generation"))

		time.Sleep(time.Hour + time.Second)
		synctest.Wait()
		require.Equal(t, []float64{1, 1}, meter.values("memcached.discovery.resolve.calls"))
		require.Equal(t, []float64{1}, meter.values("memcached.discovery.resolve.errors"))
		require.Equal(t, []float64{firstSuccess}, meter.values("memcached.discovery.resolve.last_success"),
			"failed discovery must preserve the last successful timestamp")
		require.Equal(t, []float64{2}, meter.values("memcached.topology.nodes"),
			"failed discovery must preserve the current topology")

		time.Sleep(time.Hour + time.Second)
		synctest.Wait()
		require.Equal(t, []float64{1, 1, 1}, meter.values("memcached.discovery.resolve.calls"))
		require.Equal(t, []float64{1}, meter.values("memcached.discovery.resolve.errors"))
		require.Equal(t, []float64{0.1, 0.1, 0.1}, meter.values("memcached.discovery.resolve.duration"))
		successes := meter.values("memcached.discovery.resolve.last_success")
		require.Len(t, successes, 2)
		require.Equal(t, firstSuccess, successes[0])
		require.Greater(t, successes[1], firstSuccess)
		require.Equal(t, []float64{2, 3}, meter.values("memcached.topology.nodes"))
		require.Equal(t, []float64{1, 2}, meter.values("memcached.topology.generation"))

		time.Sleep(24 * time.Hour)
		synctest.Wait()
		require.Equal(t, 3, calls, "nil schedule must stop discovery")
		require.NoError(t, c.Close())
		require.Equal(t, []float64{2, 3, 0}, meter.values("memcached.topology.nodes"))
		require.Equal(t, []float64{1, 2, 2}, meter.values("memcached.topology.generation"))
		require.NoError(t, c.Close())
		require.Equal(t, []float64{2, 3, 0}, meter.values("memcached.topology.nodes"),
			"repeated close must not emit another topology change")

		var clientID string
		for _, point := range meter.points {
			require.Equal(t, 1, point.attributes.Len())
			id, ok := point.attributes.Value("memcached.client.id")
			require.True(t, ok)
			require.NotEmpty(t, id.AsString())
			if clientID == "" {
				clientID = id.AsString()
			}
			require.Equal(t, clientID, id.AsString())
		}
	})
}

func TestClientEmitsMetricsForFailedInitialization(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		failure := errors.New("initial discovery unavailable")
		meter := &clientMetricsMeter{}
		r := metricsResolver(func(context.Context, string) (memresolver.ResolveResult, *time.Time, error) {
			time.Sleep(100 * time.Millisecond)
			return memresolver.ResolveResult{}, nil, failure
		})
		c, err := memcached.New("http://directory.example/nodes",
			memcached.WithResolver(r),
			memcached.WithTelemetry(telemetry.WithMeterProvider(clientMetricsProvider{meter: meter})),
		)
		require.ErrorIs(t, err, failure)
		require.Nil(t, c)
		require.Equal(t, []float64{1}, meter.values("memcached.discovery.resolve.calls"))
		require.Equal(t, []float64{1}, meter.values("memcached.discovery.resolve.errors"))
		require.Equal(t, []float64{0.1}, meter.values("memcached.discovery.resolve.duration"))
		for _, name := range []string{
			"memcached.discovery.resolve.last_success",
			"memcached.topology.nodes",
			"memcached.topology.generation",
		} {
			require.Empty(t, meter.values(name), "failed initialization must not emit %s", name)
		}
	})
}

func TestClientUnchangedGenerationOnlyUpdatesDiscoveryMetrics(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		meter := &clientMetricsMeter{}
		calls := 0
		resolver := metricsResolver(func(context.Context, string) (memresolver.ResolveResult, *time.Time, error) {
			calls++
			time.Sleep(100 * time.Millisecond)
			if calls == 1 {
				next := time.Now().Add(time.Hour)
				return memresolver.ResolveResult{Generation: 1, Addrs: []*memresolver.Addr{
					memresolver.NewAddr("tcp", "a.example:11211", 0),
					memresolver.NewAddr("tcp", "b.example:11211", 0),
				}}, &next, nil
			}
			// Reordering and new objects still describe the same routing topology.
			return memresolver.ResolveResult{Generation: 1, Addrs: []*memresolver.Addr{
				memresolver.NewAddr("tcp", "b.example:11211", 0),
				memresolver.NewAddr("tcp", "a.example:11211", 0),
			}}, nil, nil
		})
		c, err := memcached.New("http://directory.example/nodes",
			memcached.WithResolver(resolver),
			memcached.WithTelemetry(telemetry.WithMeterProvider(clientMetricsProvider{meter: meter})),
		)
		require.NoError(t, err)
		defer func() { require.NoError(t, c.Close()) }()
		synctest.Wait()
		firstSuccess := meter.values("memcached.discovery.resolve.last_success")
		require.Len(t, firstSuccess, 1)

		time.Sleep(time.Hour + time.Second)
		synctest.Wait()
		require.Equal(t, []float64{1, 1}, meter.values("memcached.discovery.resolve.calls"))
		require.Empty(t, meter.values("memcached.discovery.resolve.errors"))
		require.Equal(t, []float64{0.1, 0.1}, meter.values("memcached.discovery.resolve.duration"))
		successes := meter.values("memcached.discovery.resolve.last_success")
		require.Len(t, successes, 2)
		require.Equal(t, firstSuccess[0], successes[0])
		require.Greater(t, successes[1], successes[0], "unchanged topology still counts as successful discovery")
		generations := meter.values("memcached.topology.generation")
		require.NotEmpty(t, generations)
		for _, generation := range generations {
			require.Equal(t, float64(1), generation, "an unchanged source generation preserves the topology")
		}
		nodes := meter.values("memcached.topology.nodes")
		require.NotEmpty(t, nodes)
		for _, count := range nodes {
			require.Equal(t, float64(2), count)
		}
		time.Sleep(24 * time.Hour)
		synctest.Wait()
		require.Equal(t, 2, calls, "the new nil schedule must stop discovery even without membership changes")
	})
}

type metricsResolver func(context.Context, string) (memresolver.ResolveResult, *time.Time, error)

func (r metricsResolver) Resolve(ctx context.Context, target string) (memresolver.ResolveResult, *time.Time, error) {
	return r(ctx, target)
}

// The provider captures Client's emitted OTel measurements without an SDK.
type clientMetricsProvider struct {
	noop.MeterProvider
	meter *clientMetricsMeter
}

func (p clientMetricsProvider) Meter(string, ...metric.MeterOption) metric.Meter { return p.meter }

type clientMetricPoint struct {
	name       string
	value      float64
	attributes attribute.Set
}

type clientMetricsMeter struct {
	noop.Meter
	mu     sync.Mutex
	points []clientMetricPoint
}

func (m *clientMetricsMeter) Int64Counter(name string, _ ...metric.Int64CounterOption) (metric.Int64Counter, error) {
	return clientIntRecorder{meter: m, name: name}, nil
}

func (m *clientMetricsMeter) Int64Gauge(name string, _ ...metric.Int64GaugeOption) (metric.Int64Gauge, error) {
	return clientIntRecorder{meter: m, name: name}, nil
}

func (m *clientMetricsMeter) Float64Histogram(name string, _ ...metric.Float64HistogramOption) (metric.Float64Histogram, error) {
	return clientFloatRecorder{meter: m, name: name}, nil
}

func (m *clientMetricsMeter) Float64Gauge(name string, _ ...metric.Float64GaugeOption) (metric.Float64Gauge, error) {
	return clientFloatRecorder{meter: m, name: name}, nil
}

func (m *clientMetricsMeter) capture(name string, value float64, attributes attribute.Set) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.points = append(m.points, clientMetricPoint{name: name, value: value, attributes: attributes})
}

func (m *clientMetricsMeter) values(name string) []float64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	var values []float64
	for _, point := range m.points {
		if point.name == name {
			values = append(values, point.value)
		}
	}
	return values
}

type clientIntRecorder struct {
	noop.Int64Counter
	noop.Int64Gauge
	meter *clientMetricsMeter
	name  string
}

func (clientIntRecorder) Enabled(context.Context) bool { return true }

func (r clientIntRecorder) Add(_ context.Context, value int64, options ...metric.AddOption) {
	config := metric.NewAddConfig(options)
	r.meter.capture(r.name, float64(value), config.Attributes())
}

func (r clientIntRecorder) Record(_ context.Context, value int64, options ...metric.RecordOption) {
	config := metric.NewRecordConfig(options)
	r.meter.capture(r.name, float64(value), config.Attributes())
}

type clientFloatRecorder struct {
	noop.Float64Histogram
	noop.Float64Gauge
	meter *clientMetricsMeter
	name  string
}

func (clientFloatRecorder) Enabled(context.Context) bool { return true }

func (r clientFloatRecorder) Record(_ context.Context, value float64, options ...metric.RecordOption) {
	config := metric.NewRecordConfig(options)
	r.meter.capture(r.name, value, config.Attributes())
}
