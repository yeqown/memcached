// Command otel demonstrates OpenTelemetry integration with memcached client.
// Run with: go run main.go
package main

import (
	"context"
	"log"
	"time"

	"github.com/yeqown/memcached"
	"github.com/yeqown/memcached/telemetry"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/stdout/stdoutmetric"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/trace"
)

func main() {
	// Create STDOUT exporter for tracing
	traceExporter, err := stdouttrace.New(
		stdouttrace.WithPrettyPrint(),
	)
	if err != nil {
		log.Fatal(err)
	}

	// Create STDOUT exporter for metrics
	metricExporter, err := stdoutmetric.New()
	if err != nil {
		log.Fatal(err)
	}

	// Create tracer provider
	tp := trace.NewTracerProvider(
		trace.WithBatcher(traceExporter),
	)
	defer tp.Shutdown(context.Background())

	// Create meter provider
	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(metricExporter)),
	)
	defer mp.Shutdown(context.Background())

	// Set global providers
	otel.SetTracerProvider(tp)
	otel.SetMeterProvider(mp)

	// Create memcached client with telemetry enabled
	client, err := memcached.New("localhost:11211",
		memcached.WithTelemetry(
			telemetry.WithTracerProvider(tp),
			telemetry.WithMeterProvider(mp),
		),
	)
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close()

	ctx := context.Background()

	// Test SET operation
	log.Println("=== Testing SET ===")
	err = client.Set(ctx, "test_key", []byte("test_value"), 0, 0)
	if err != nil {
		log.Printf("SET error: %v", err)
	} else {
		log.Println("SET success")
	}

	// Test GET operation
	log.Println("\n=== Testing GET ===")
	item, err := client.Get(ctx, "test_key")
	if err != nil {
		log.Printf("GET error: %v", err)
	} else {
		log.Printf("GET success: %s", string(item.Value))
	}

	// Test DELETE operation
	log.Println("\n=== Testing DELETE ===")
	err = client.Delete(ctx, "test_key")
	if err != nil {
		log.Printf("DELETE error: %v", err)
	} else {
		log.Println("DELETE success")
	}

	// Flush to ensure all telemetry data is exported
	time.Sleep(100 * time.Millisecond)
	log.Println("\n=== Check output above for traces and metrics ===")
}
