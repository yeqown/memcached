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

// newTracer creates a new Tracer with the given tracer provider.
// If tp is nil, it uses the global tracer provider.
func newTracer(tp trace.TracerProvider) *Tracer {
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
// A nil Tracer returns the original context and a nil span.
func (t *Tracer) Start(ctx context.Context, operation, server, network string, key string) (context.Context, trace.Span) {
	if t == nil {
		return ctx, nil
	}

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
// A nil Tracer does nothing.
func (t *Tracer) End(span trace.Span, err error) {
	if t == nil {
		return
	}

	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		span.RecordError(err)
	} else {
		span.SetStatus(codes.Ok, "")
	}
	span.End()
}
