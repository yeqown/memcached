package telemetry

import (
	"context"
	"strconv"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// metricsClientSequence assigns a stable identity only when metrics are enabled.
var metricsClientSequence atomic.Uint64

// Metrics holds OpenTelemetry metric instruments for memcached operations and discovery.
type Metrics struct {
	operationDuration  metric.Float64Histogram
	operationCalls     metric.Int64Counter
	operationErrors    metric.Int64Counter
	resolveDuration    metric.Float64Histogram
	resolveCalls       metric.Int64Counter
	resolveErrors      metric.Int64Counter
	resolveSuccess     metric.Int64Gauge
	topologyNodes      metric.Int64Gauge
	topologyGeneration metric.Float64Gauge
	clientAttributes   attribute.Set
}

// newMetrics creates a new Metrics with the given meter provider.
// If mp is nil, it uses the global meter provider.
func newMetrics(mp metric.MeterProvider) (*Metrics, error) {
	if mp == nil {
		mp = otel.GetMeterProvider()
	}
	meter := mp.Meter(
		"github.com/yeqown/memcached",
		metric.WithInstrumentationVersion("1.0.0"),
	)

	duration, err := meter.Float64Histogram(
		"memcached.operation.duration",
		metric.WithUnit("s"),
		metric.WithDescription("Duration of memcached operations"),
	)
	if err != nil {
		return nil, err
	}

	calls, err := meter.Int64Counter(
		"memcached.operation.calls",
		metric.WithUnit("{call}"),
		metric.WithDescription("Number of memcached operations"),
	)
	if err != nil {
		return nil, err
	}

	errors, err := meter.Int64Counter(
		"memcached.operation.errors",
		metric.WithUnit("{error}"),
		metric.WithDescription("Number of memcached operation errors"),
	)
	if err != nil {
		return nil, err
	}

	resolveDuration, err := meter.Float64Histogram(
		"memcached.discovery.resolve.duration",
		metric.WithUnit("s"),
		metric.WithDescription("Duration of discovery resolve and topology application attempts"),
	)
	if err != nil {
		return nil, err
	}

	resolveCalls, err := meter.Int64Counter(
		"memcached.discovery.resolve.calls",
		metric.WithUnit("{call}"),
		metric.WithDescription("Number of discovery resolve attempts"),
	)
	if err != nil {
		return nil, err
	}

	resolveErrors, err := meter.Int64Counter(
		"memcached.discovery.resolve.errors",
		metric.WithUnit("{error}"),
		metric.WithDescription("Number of failed discovery resolve or topology application attempts"),
	)
	if err != nil {
		return nil, err
	}

	resolveSuccess, err := meter.Int64Gauge(
		"memcached.discovery.resolve.last_success",
		metric.WithUnit("s"),
		metric.WithDescription("Unix timestamp of the last successful discovery resolve and topology application"),
	)
	if err != nil {
		return nil, err
	}

	topologyNodes, err := meter.Int64Gauge(
		"memcached.topology.nodes",
		metric.WithUnit("{node}"),
		metric.WithDescription("Number of nodes in the client's current topology"),
	)
	if err != nil {
		return nil, err
	}

	topologyGeneration, err := meter.Float64Gauge(
		"memcached.topology.generation",
		metric.WithDescription("Generation of the client's current topology"),
	)
	if err != nil {
		return nil, err
	}

	return &Metrics{
		operationDuration:  duration,
		operationCalls:     calls,
		operationErrors:    errors,
		resolveDuration:    resolveDuration,
		resolveCalls:       resolveCalls,
		resolveErrors:      resolveErrors,
		resolveSuccess:     resolveSuccess,
		topologyNodes:      topologyNodes,
		topologyGeneration: topologyGeneration,
		clientAttributes: attribute.NewSet(attribute.String(
			"memcached.client.id", strconv.FormatUint(metricsClientSequence.Add(1), 10),
		)),
	}, nil
}

// RecordResolve records one discovery attempt, including validation and topology
// application. A nil error means the result was successfully applied (including
// an unchanged topology), and records the current Unix timestamp in seconds.
// Failures increment the error counter without replacing the last success time.
// Discovery measurements use a stable generated client ID, never a discovery
// target, source version, or node list, to keep clients' gauge values separate.
func (m *Metrics) RecordResolve(ctx context.Context, duration time.Duration, err error) {
	if m == nil {
		return
	}

	attrs := metric.WithAttributeSet(m.clientAttributes)
	m.resolveCalls.Add(ctx, 1, attrs)
	m.resolveDuration.Record(ctx, duration.Seconds(), attrs)
	if err != nil {
		m.resolveErrors.Add(ctx, 1, attrs)
		return
	}
	m.resolveSuccess.Record(ctx, time.Now().Unix(), attrs)
}

// RecordTopology records the current node count and generation after applying a
// topology, together with positive membership additions and removals. Call it
// with zero nodes when the client closes. Gauges use the same stable client ID
// as RecordResolve, allowing clients sharing a provider to report independently.
// Generation uses a floating-point gauge to represent the unsigned generation
// without becoming negative when it exceeds the signed integer range.
func (m *Metrics) RecordTopology(ctx context.Context, nodes int, generation uint64) {
	if m == nil {
		return
	}

	attrs := metric.WithAttributeSet(m.clientAttributes)
	m.topologyNodes.Record(ctx, int64(nodes), attrs)
	m.topologyGeneration.Record(ctx, float64(generation), attrs)
}

// RecordDuration records the operation duration.
func (m *Metrics) RecordDuration(ctx context.Context, operation, server string, duration time.Duration, err error) {
	attrs := []attribute.KeyValue{
		attrDBSystem.String("memcached"),
		attrDBOperation.String(operation),
		attrNetPeerName.String(server),
	}

	m.operationCalls.Add(ctx, 1, metric.WithAttributes(attrs...))
	m.operationDuration.Record(ctx, duration.Seconds(), metric.WithAttributes(attrs...))

	if err != nil {
		m.operationErrors.Add(ctx, 1, metric.WithAttributes(attrs...))
	}
}
