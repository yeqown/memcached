// Package testutil provides shared fixtures for repository tests.
package testutil

import (
	"context"
	"slices"
	"sync"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/metric/noop"
)

// Measurement is one observation at the OpenTelemetry API boundary.
type Measurement struct {
	Name       string
	Value      float64
	Attributes attribute.Set
}

// CaptureMeter records measurements without an SDK or aggregation behavior.
// Configure FailName and Failure before use to reject an instrument registration.
type CaptureMeter struct {
	noop.Meter
	FailName string
	Failure  error

	mu           sync.Mutex
	measurements []Measurement
}

// Provider returns a meter provider backed by this capture.
func (m *CaptureMeter) Provider() metric.MeterProvider { return captureProvider{meter: m} }

// Measurements returns a snapshot that can be read while instruments record.
func (m *CaptureMeter) Measurements() []Measurement {
	m.mu.Lock()
	defer m.mu.Unlock()
	return slices.Clone(m.measurements)
}

// Values returns observations for one instrument, in recording order.
func (m *CaptureMeter) Values(name string) []float64 {
	var values []float64
	for _, measurement := range m.Measurements() {
		if measurement.Name == name {
			values = append(values, measurement.Value)
		}
	}
	return values
}

func (m *CaptureMeter) Int64Counter(name string, _ ...metric.Int64CounterOption) (metric.Int64Counter, error) {
	if name == m.FailName {
		return nil, m.Failure
	}
	return intRecorder{meter: m, name: name}, nil
}

func (m *CaptureMeter) Int64Gauge(name string, _ ...metric.Int64GaugeOption) (metric.Int64Gauge, error) {
	if name == m.FailName {
		return nil, m.Failure
	}
	return intRecorder{meter: m, name: name}, nil
}

func (m *CaptureMeter) Float64Histogram(name string, _ ...metric.Float64HistogramOption) (metric.Float64Histogram, error) {
	if name == m.FailName {
		return nil, m.Failure
	}
	return floatRecorder{meter: m, name: name}, nil
}

func (m *CaptureMeter) Float64Gauge(name string, _ ...metric.Float64GaugeOption) (metric.Float64Gauge, error) {
	if name == m.FailName {
		return nil, m.Failure
	}
	return floatRecorder{meter: m, name: name}, nil
}

func (m *CaptureMeter) capture(name string, value float64, attributes attribute.Set) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.measurements = append(m.measurements, Measurement{Name: name, Value: value, Attributes: attributes})
}

type captureProvider struct {
	noop.MeterProvider
	meter *CaptureMeter
}

func (p captureProvider) Meter(string, ...metric.MeterOption) metric.Meter { return p.meter }

type intRecorder struct {
	noop.Int64Counter
	noop.Int64Gauge
	meter *CaptureMeter
	name  string
}

func (intRecorder) Enabled(context.Context) bool { return true }

func (r intRecorder) Add(_ context.Context, value int64, options ...metric.AddOption) {
	config := metric.NewAddConfig(options)
	r.meter.capture(r.name, float64(value), config.Attributes())
}

func (r intRecorder) Record(_ context.Context, value int64, options ...metric.RecordOption) {
	config := metric.NewRecordConfig(options)
	r.meter.capture(r.name, float64(value), config.Attributes())
}

type floatRecorder struct {
	noop.Float64Histogram
	noop.Float64Gauge
	meter *CaptureMeter
	name  string
}

func (floatRecorder) Enabled(context.Context) bool { return true }

func (r floatRecorder) Record(_ context.Context, value float64, options ...metric.RecordOption) {
	config := metric.NewRecordConfig(options)
	r.meter.capture(r.name, value, config.Attributes())
}
