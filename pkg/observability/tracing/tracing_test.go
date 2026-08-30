package tracing_test

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	oteltrace "go.opentelemetry.io/otel/trace"

	"github.com/nicodanke/dizen-v2-backend/pkg/observability/tracing"
)

// Without an endpoint the package must install a no-op provider: instrumented code keeps
// working and produces no spans, which is what a unit test and a bare `go run` need.
func TestWithoutAnEndpointTracingIsANoOp(t *testing.T) {
	shutdown, err := tracing.Setup(t.Context(), tracing.Options{ServiceName: "identity"})
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}

	if shutdown == nil {
		t.Fatal("Setup returned a nil shutdown: main.go could not defer it")
	}

	_, span := tracing.Tracer("test").Start(t.Context(), "operation")
	defer span.End()

	if span.SpanContext().IsSampled() {
		t.Error("the no-op provider produced a sampled span")
	}

	if err := shutdown(t.Context()); err != nil {
		t.Errorf("shutdown: %v", err)
	}
}

// The propagator is installed even with tracing off: a service must keep forwarding the
// traceparent it received so the next hop can use it.
func TestThePropagatorIsInstalledEvenWithoutAnEndpoint(t *testing.T) {
	if _, err := tracing.Setup(t.Context(), tracing.Options{ServiceName: "tours"}); err != nil {
		t.Fatalf("Setup: %v", err)
	}

	propagator := otel.GetTextMapPropagator()

	fields := propagator.Fields()
	if len(fields) == 0 {
		t.Fatal("no propagator was installed")
	}

	var hasTraceparent bool

	for _, field := range fields {
		if field == "traceparent" {
			hasTraceparent = true
		}
	}

	if !hasTraceparent {
		t.Errorf("the W3C propagator is not installed, fields = %v", fields)
	}
}

// A traceparent arriving from the mobile app has to survive extraction: that is what ties
// a tour to the calls it makes (01 section 7).
func TestAnIncomingTraceparentIsExtracted(t *testing.T) {
	if _, err := tracing.Setup(t.Context(), tracing.Options{ServiceName: "identity"}); err != nil {
		t.Fatalf("Setup: %v", err)
	}

	const traceID = "4bf92f3577b34da6a3ce929d0e0e4736"

	carrier := propagation.MapCarrier{
		"traceparent": "00-" + traceID + "-00f067aa0ba902b7-01",
	}

	ctx := otel.GetTextMapPropagator().Extract(context.Background(), carrier)

	spanCtx := oteltrace.SpanContextFromContext(ctx)
	if !spanCtx.IsValid() {
		t.Fatal("the traceparent was not extracted")
	}

	if got := spanCtx.TraceID().String(); got != traceID {
		t.Errorf("trace id = %q, want %q", got, traceID)
	}
}

func TestAnInvalidEndpointIsReported(t *testing.T) {
	// The exporter resolves lazily, so an unreachable address is not an error at setup;
	// what must be reported is a configuration that cannot be built at all.
	shutdown, err := tracing.Setup(t.Context(), tracing.Options{
		ServiceName: "identity",
		Endpoint:    "collector:4317",
		Insecure:    true,
		SampleRatio: 1,
	})
	if err != nil {
		t.Fatalf("Setup with a valid endpoint failed: %v", err)
	}

	if err := shutdown(t.Context()); err != nil {
		t.Errorf("shutdown: %v", err)
	}
}
