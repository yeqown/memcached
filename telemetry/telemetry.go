// Package telemetry provides OpenTelemetry integration for the memcached client.
//
// It supports both tracing and metrics with configurable exporters.
// By default, telemetry is disabled (no-op) to ensure zero overhead.
package telemetry

import (
	"log"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

// Semantic attribute keys following OpenTelemetry Database Semantic Conventions
var (
	attrDBSystem     = attribute.Key("db.system")
	attrDBOperation  = attribute.Key("db.operation")
	attrNetPeerName  = attribute.Key("net.peer.name")
	attrNetPeerPort  = attribute.Key("net.peer.port")
	attrNetTransport = attribute.Key("net.transport")
	attrMemcachedKey = attribute.Key("memcached.key")
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
func NewConfig(opts ...Option) *Config {
	c := &Config{
		TracerProvider: nil,
		MeterProvider:  nil,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

func (c *Config) Tracer() *Tracer {
	if c == nil || c.TracerProvider == nil {
		return nil
	}

	return newTracer(c.TracerProvider)
}

func (c *Config) Metics() *Metrics {
	if c == nil || c.MeterProvider == nil {
		return nil
	}

	m, err := newMetrics(c.MeterProvider)
	if err != nil {
		log.Printf("[memcached.telemetry] failed to create metrics: %v", err)
		return nil
	}

	return m
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
