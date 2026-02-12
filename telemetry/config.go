package telemetry

import (
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

// Option configures the Telemetry behavior.
type Option func(*Config)

// Config holds the OpenTelemetry configuration.
type Config struct {
	TracerProvider trace.TracerProvider
	MeterProvider  metric.MeterProvider
}

// NewConfig creates a new Config with the given options.
// It uses no-op providers by default for zero overhead when telemetry is disabled.
// Setting a provider implicitly enables the corresponding telemetry feature.
func NewConfig(opts []Option) *Config {
	c := &Config{
		TracerProvider: nil,
		MeterProvider:  nil,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// WithTracerProvider sets the tracer provider and enables tracing.
// If tp is nil, the global tracer provider will be used.
func WithTracerProvider(tp trace.TracerProvider) Option {
	return func(c *Config) {
		c.TracerProvider = tp
	}
}

// WithMeterProvider sets the meter provider and enables metrics.
// If mp is nil, the global meter provider will be used.
func WithMeterProvider(mp metric.MeterProvider) Option {
	return func(c *Config) {
		c.MeterProvider = mp
	}
}
