package interceptor

import (
	"context"
	"runtime/debug"

	"github.com/rs/zerolog"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/nicodanke/dizen-v2-backend/pkg/logger"
)

// panicMessage is what the client is told when a handler panics. It carries no detail on
// purpose: a stack trace in a response is an information leak.
const panicMessage = "internal error"

// Recovery converts a panic in a handler into INTERNAL without leaking anything, and logs
// the panic value plus the full stack at error level together with the trace_id.
//
// This is acceptance criterion 5 of PRD-00. It is first in the chain (03 section 7) so it
// also covers a panic raised by any interceptor below it.
//
// The logger is passed in rather than read from the context precisely because of that
// position: the logging interceptor runs *after* this one, so when a panic unwinds back up
// here the context does not carry a logger yet. The trace identifiers are still read from
// the context, since tracing is installed as a StatsHandler and runs before any
// interceptor.
func Recovery(log zerolog.Logger) grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		req any,
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (resp any, err error) {
		defer func() {
			if r := recover(); r != nil {
				logPanic(ctx, log, info.FullMethod, r)

				// The named return is overwritten so the client gets a clean INTERNAL
				// even though the handler never returned.
				resp = nil
				err = status.Error(codes.Internal, panicMessage)
			}
		}()

		return handler(ctx, req)
	}
}

// RecoveryStream is the streaming counterpart of Recovery.
func RecoveryStream(log zerolog.Logger) grpc.StreamServerInterceptor {
	return func(
		srv any,
		ss grpc.ServerStream,
		info *grpc.StreamServerInfo,
		handler grpc.StreamHandler,
	) (err error) {
		defer func() {
			if r := recover(); r != nil {
				logPanic(ss.Context(), log, info.FullMethod, r)

				err = status.Error(codes.Internal, panicMessage)
			}
		}()

		return handler(srv, ss)
	}
}

// logPanic writes everything the client is not told: the panic value and the full stack,
// tied to the trace so the incident can be reconstructed.
func logPanic(ctx context.Context, log zerolog.Logger, method string, recovered any) {
	event := log.Error().
		Str(logger.FieldMethod, method).
		Interface("panic", recovered).
		Bytes("stack", debug.Stack())

	if traceID := logger.TraceID(ctx); traceID != "" {
		event = event.Str(logger.FieldTraceID, traceID)
	}

	event.Msg("recovered a panic in a gRPC handler")
}
