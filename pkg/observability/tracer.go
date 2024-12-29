package observability

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/jaeger"
	"go.opentelemetry.io/otel/sdk/resource"
	tracesdk "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.4.0"
	"go.opentelemetry.io/otel/trace"
)

// InitTracer configures the Jaeger tracer
func InitTracer(serviceName, jaegerEndpoint string) (func(), error) {
	// Create Jaeger exporter
	exp, err := jaeger.New(jaeger.WithCollectorEndpoint(jaeger.WithEndpoint(jaegerEndpoint)))
	if err != nil {
		return nil, err
	}

	// Configure resource
	res, err := resource.New(
		context.Background(),
		resource.WithAttributes(
			semconv.ServiceNameKey.String(serviceName),
		),
	)
	if err != nil {
		return nil, err
	}

	// Create TracerProvider
	tp := tracesdk.NewTracerProvider(
		tracesdk.WithBatcher(exp),
		tracesdk.WithResource(res),
	)

	// Set global TracerProvider
	otel.SetTracerProvider(tp)

	// Return cleanup function
	return func() {
		if err := tp.Shutdown(context.Background()); err != nil {
			log := Logger()
			log.Error().Err(err).Msg("Error shutting down tracer provider")
		}
	}, nil
}

// Tracer returns a new tracer instance
func Tracer(name string) trace.Tracer {
	return otel.Tracer(name)
}
