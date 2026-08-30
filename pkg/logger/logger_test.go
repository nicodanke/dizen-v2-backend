package logger_test

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/rs/zerolog"
	"go.opentelemetry.io/otel/trace"

	"github.com/nicodanke/dizen-v2-backend/pkg/logger"
)

// decode parses the last JSON line written to the buffer.
func decode(t *testing.T, buf *bytes.Buffer) map[string]any {
	t.Helper()

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	last := lines[len(lines)-1]

	var entry map[string]any
	if err := json.Unmarshal([]byte(last), &entry); err != nil {
		t.Fatalf("the line is not valid JSON (%q): %v", last, err)
	}

	return entry
}

func TestNewEmitsJSONWithServiceAndVersion(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	log := logger.New(logger.Options{ServiceName: "identity", Level: "info", Output: &buf})
	log.Info().Msg("started")

	entry := decode(t, &buf)

	if entry[logger.FieldService] != "identity" {
		t.Errorf("service = %v, want identity", entry[logger.FieldService])
	}

	if entry[logger.FieldVersion] == nil {
		t.Error("the version field is missing")
	}

	if entry["message"] != "started" {
		t.Errorf("message = %v", entry["message"])
	}

	if entry["time"] == nil {
		t.Error("the timestamp is missing")
	}
}

func TestTheLevelFiltersLowerEntries(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	log := logger.New(logger.Options{ServiceName: "tours", Level: "warn", Output: &buf})
	log.Debug().Msg("invisible")
	log.Info().Msg("also invisible")

	if buf.Len() != 0 {
		t.Errorf("entries below warn were emitted: %s", buf.String())
	}

	log.Warn().Msg("visible")

	if decode(t, &buf)["message"] != "visible" {
		t.Error("the warn entry was not emitted")
	}
}

func TestPrettyProducesConsoleOutputInsteadOfJSON(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	log := logger.New(logger.Options{ServiceName: "booking", Level: "info", Pretty: true, Output: &buf})
	log.Info().Msg("hello")

	out := buf.String()

	if json.Valid([]byte(strings.TrimSpace(out))) {
		t.Errorf("the pretty writer produced JSON: %s", out)
	}

	if !strings.Contains(out, "hello") {
		t.Errorf("the message is missing from the console output: %s", out)
	}
}

func TestParseLevelFallsBackToInfo(t *testing.T) {
	t.Parallel()

	cases := map[string]zerolog.Level{
		"debug":     zerolog.DebugLevel,
		"WARN":      zerolog.WarnLevel,
		"  error  ": zerolog.ErrorLevel,
		"nonsense":  zerolog.InfoLevel,
		"":          zerolog.InfoLevel,
	}

	for input, want := range cases {
		if got := logger.ParseLevel(input); got != want {
			t.Errorf("ParseLevel(%q) = %v, want %v", input, got, want)
		}
	}
}

func TestFromContextReturnsADisabledLoggerWhenThereIsNone(t *testing.T) {
	t.Parallel()

	// It must not panic and must not write: a handler calling the logger before it is
	// injected should stay silent, not crash.
	log := logger.FromContext(context.Background())
	log.Error().Msg("goes nowhere")
}

func TestWithContextRoundTrips(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	log := logger.New(logger.Options{ServiceName: "admin", Level: "info", Output: &buf})
	ctx := logger.WithContext(context.Background(), log)

	logger.FromContext(ctx).Info().Msg("from the context")

	if decode(t, &buf)["message"] != "from the context" {
		t.Error("the logger did not survive the round trip through the context")
	}
}

// spanContext builds a valid span context so the trace enrichment can be exercised without
// starting a real tracer.
func spanContext(t *testing.T) trace.SpanContext {
	t.Helper()

	traceID, err := trace.TraceIDFromHex("4bf92f3577b34da6a3ce929d0e0e4736")
	if err != nil {
		t.Fatalf("trace id: %v", err)
	}

	spanID, err := trace.SpanIDFromHex("00f067aa0ba902b7")
	if err != nil {
		t.Fatalf("span id: %v", err)
	}

	return trace.NewSpanContext(trace.SpanContextConfig{
		TraceID: traceID,
		SpanID:  spanID,
		Remote:  true,
	})
}

// This is the RF-4 acceptance criterion: trace_id and span_id are injected automatically,
// which is what correlates a log line with its trace.
func TestCtxInjectsTraceAndSpanIdentifiers(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	log := logger.New(logger.Options{ServiceName: "tours", Level: "info", Output: &buf})
	ctx := trace.ContextWithSpanContext(
		logger.WithContext(context.Background(), log),
		spanContext(t),
	)

	logger.Ctx(ctx).Info().Msg("with a trace")

	entry := decode(t, &buf)

	if entry[logger.FieldTraceID] != "4bf92f3577b34da6a3ce929d0e0e4736" {
		t.Errorf("trace_id = %v", entry[logger.FieldTraceID])
	}

	if entry[logger.FieldSpanID] != "00f067aa0ba902b7" {
		t.Errorf("span_id = %v", entry[logger.FieldSpanID])
	}
}

func TestCtxOmitsTheIdentifiersWithoutASpan(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	log := logger.New(logger.Options{ServiceName: "tours", Level: "info", Output: &buf})
	ctx := logger.WithContext(context.Background(), log)

	logger.Ctx(ctx).Info().Msg("no trace")

	entry := decode(t, &buf)

	if _, ok := entry[logger.FieldTraceID]; ok {
		t.Error("trace_id was emitted without an active span")
	}
}

func TestTraceID(t *testing.T) {
	t.Parallel()

	if got := logger.TraceID(context.Background()); got != "" {
		t.Errorf("TraceID without a span = %q, want empty", got)
	}

	ctx := trace.ContextWithSpanContext(context.Background(), spanContext(t))

	if got := logger.TraceID(ctx); got != "4bf92f3577b34da6a3ce929d0e0e4736" {
		t.Errorf("TraceID = %q", got)
	}
}
