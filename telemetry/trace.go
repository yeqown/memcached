package telemetry

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// Tracer wraps an OpenTelemetry tracer for memcached operations.
type Tracer struct {
	tracer trace.Tracer
}

// NewTracer creates a new Tracer with the given tracer provider.
// If tp is nil, it uses the global tracer provider.
func NewTracer(tp trace.TracerProvider) *Tracer {
	if tp == nil {
		tp = otel.GetTracerProvider()
	}
	return &Tracer{
		tracer: tp.Tracer(
			"github.com/yeqown/memcached",
			trace.WithInstrumentationVersion("1.0.0"),
		),
	}
}

// Start creates a span for a memcached operation.
func (t *Tracer) Start(ctx context.Context, operation, server, network string, key string) (context.Context, trace.Span) {
	attrs := []attribute.KeyValue{
		attrDBSystem.String("memcached"),
		attrDBOperation.String(operation),
		attrNetPeerName.String(server),
		attrNetTransport.String(network),
	}
	if key != "" {
		attrs = append(attrs, attrMemcachedKey.String(key))
	}
	return t.tracer.Start(ctx, "memcached."+operation,
		trace.WithAttributes(attrs...),
		trace.WithSpanKind(trace.SpanKindClient),
	)
}

// End finishes the span with appropriate status.
func (t *Tracer) End(span trace.Span, err error) {
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		span.RecordError(err)
	} else {
		span.SetStatus(codes.Ok, "")
	}
	span.End()
}
