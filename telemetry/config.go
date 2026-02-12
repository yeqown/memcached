package telemetry

import (
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

// Option configures the Telemetry behavior.
type Option func(*Config)

// Config holds the OpenTelemetry configuration.
type Config struct {
	TracerProvider trace.TracerProvider
	MeterProvider  metric.MeterProvider
	EnableTracing  bool
	EnableMetrics  bool
}

func NewConfig(opts []Option) *Config {
	c := &Config{
		TracerProvider: otel.GetTracerProvider(),
		MeterProvider:  otel.GetMeterProvider(),
		EnableTracing:  false,
		EnableMetrics:  false,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// WithTracing enables distributed tracing.
func WithTracing() Option {
	return func(c *Config) {
		c.EnableTracing = true
	}
}

// WithMetrics enables metrics collection.
func WithMetrics() Option {
	return func(c *Config) {
		c.EnableMetrics = true
	}
}

// WithTracerProvider sets a custom tracer provider.
func WithTracerProvider(tp trace.TracerProvider) Option {
	return func(c *Config) {
		c.TracerProvider = tp
	}
}

// WithMeterProvider sets a custom meter provider.
func WithMeterProvider(mp metric.MeterProvider) Option {
	return func(c *Config) {
		c.MeterProvider = mp
	}
}
