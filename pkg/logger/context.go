package logger

import (
	"context"

	"github.com/rs/zerolog"
	"go.opentelemetry.io/otel/trace"
)

// contextKey is unexported so no other package can collide with this key.
type contextKey struct{}

// nop is the logger handed out when the context carries none. It is package level because
// FromContext returns a pointer and must not hand out a pointer to a local.
var nop = Nop()

// WithContext stores the logger in the context so handlers can reach it without it being
// threaded through every signature.
func WithContext(ctx context.Context, log zerolog.Logger) context.Context {
	return context.WithValue(ctx, contextKey{}, log)
}

// FromContext returns the logger stored in the context, or a disabled logger when there is
// none. It never returns nil, so callers never have to guard the call.
//
// A pointer is returned because zerolog declares its level methods on *Logger: returning a
// value would force every caller to assign it to a variable first.
func FromContext(ctx context.Context) *zerolog.Logger {
	log, ok := ctx.Value(contextKey{}).(zerolog.Logger)
	if !ok {
		return &nop
	}

	return &log
}

// Ctx returns the context logger enriched with the identifiers of the active span.
//
// This is what correlates a log line with its trace in Jaeger and with the error Sentry
// reports (01 section 7). It is the accessor handlers should use; FromContext is the raw
// one, without trace enrichment.
func Ctx(ctx context.Context) *zerolog.Logger {
	log := FromContext(ctx)

	spanCtx := trace.SpanContextFromContext(ctx)
	if !spanCtx.IsValid() {
		return log
	}

	enriched := log.With().
		Str(FieldTraceID, spanCtx.TraceID().String()).
		Str(FieldSpanID, spanCtx.SpanID().String()).
		Logger()

	return &enriched
}

// TraceID returns the identifier of the active trace, or an empty string when there is no
// valid span. Used to include the trace in an error returned to the client so a support
// report can be tied to its logs.
func TraceID(ctx context.Context) string {
	spanCtx := trace.SpanContextFromContext(ctx)
	if !spanCtx.IsValid() {
		return ""
	}

	return spanCtx.TraceID().String()
}
