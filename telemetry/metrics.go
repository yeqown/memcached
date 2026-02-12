package telemetry

import (
	"context"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// Metrics holds OpenTelemetry metric instruments for memcached operations.
type Metrics struct {
	operationDuration metric.Float64Histogram
	operationCalls    metric.Int64Counter
	operationErrors   metric.Int64Counter
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

	return &Metrics{
		operationDuration: duration,
		operationCalls:    calls,
		operationErrors:   errors,
	}, nil
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
