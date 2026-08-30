// Package tracing wires OpenTelemetry: an OTLP exporter to Jaeger and the W3C propagator
// that carries traceparent from the mobile app through every hop (01 section 7).
package tracing

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"

	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

// Options configures the tracer provider.
type Options struct {
	// ServiceName identifies the service in Jaeger.
	ServiceName string

	// ServiceVersion is reported as a resource attribute so a regression can be tied to
	// the release that introduced it.
	ServiceVersion string

	// Environment separates local, staging and production traces.
	Environment string

	// Endpoint is the OTLP collector address (host:port). Empty disables tracing.
	Endpoint string

	// SampleRatio is the fraction of traces sampled, between 0 and 1.
	SampleRatio float64

	// Insecure skips TLS to the collector. True inside the docker network, false when the
	// collector is reachable over the internet.
	Insecure bool
}

// Shutdown flushes and releases the provider. It always returns non-nil so main.go can
// defer it without a nil check.
type Shutdown func(context.Context) error

// Setup installs the global tracer provider and propagator.
//
// With no endpoint configured it installs a no-op provider and returns a no-op shutdown:
// instrumented code keeps working, it just produces no spans. That is what unit tests and
// a bare `go run` need, and it means no call site has to check whether tracing is on.
func Setup(ctx context.Context, opts Options) (Shutdown, error) {
	// The propagator is installed either way: even without an exporter, a service must
	// keep forwarding the traceparent it received so the next hop can use it.
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	if opts.Endpoint == "" {
		otel.SetTracerProvider(noop.NewTracerProvider())

		return func(context.Context) error { return nil }, nil
	}

	exporter, err := newExporter(ctx, opts)
	if err != nil {
		return nil, err
	}

	res, err := newResource(ctx, opts)
	if err != nil {
		return nil, err
	}

	provider := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter, sdktrace.WithBatchTimeout(5*time.Second)),
		sdktrace.WithResource(res),
		// ParentBased keeps the decision the caller already made: if the mobile app
		// sampled a trace, every hop of it is sampled.
		sdktrace.WithSampler(sdktrace.ParentBased(
			sdktrace.TraceIDRatioBased(opts.SampleRatio),
		)),
	)

	otel.SetTracerProvider(provider)

	return provider.Shutdown, nil
}

// newExporter builds the OTLP/gRPC exporter.
func newExporter(ctx context.Context, opts Options) (sdktrace.SpanExporter, error) {
	exporterOpts := []otlptracegrpc.Option{
		otlptracegrpc.WithEndpoint(opts.Endpoint),
	}

	if opts.Insecure {
		exporterOpts = append(exporterOpts, otlptracegrpc.WithInsecure())
	}

	exporter, err := otlptracegrpc.New(ctx, exporterOpts...)
	if err != nil {
		return nil, fmt.Errorf("building the OTLP exporter for %q: %w", opts.Endpoint, err)
	}

	return exporter, nil
}

// newResource describes the service that emits the spans.
func newResource(ctx context.Context, opts Options) (*resource.Resource, error) {
	res, err := resource.New(ctx,
		resource.WithFromEnv(),
		resource.WithProcess(),
		resource.WithAttributes(
			semconv.ServiceName(opts.ServiceName),
			semconv.ServiceVersion(opts.ServiceVersion),
			attribute.String("deployment.environment", opts.Environment),
		),
	)

	// A schema mismatch between detectors is not fatal: the resource is still usable and
	// refusing to start over it would be worse than losing an attribute.
	if err != nil && !errors.Is(err, resource.ErrSchemaURLConflict) {
		return nil, fmt.Errorf("building the trace resource: %w", err)
	}

	return res, nil
}

// Tracer returns a named tracer from the global provider.
func Tracer(name string) trace.Tracer {
	return otel.Tracer(name)
}
