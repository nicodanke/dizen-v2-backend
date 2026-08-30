package interceptor

import (
	"context"
	"time"

	"github.com/rs/zerolog"
	"google.golang.org/grpc"
	"google.golang.org/grpc/status"

	"github.com/nicodanke/dizen-v2-backend/pkg/logger"
)

// Logging injects the request logger into the context and writes one line per call with
// the method, latency, status code, trace_id and, once auth lands, the user_id
// (03 section 7).
//
// It sits above metrics so that a call rejected further down the chain is still logged.
func Logging(log zerolog.Logger) grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		req any,
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (any, error) {
		start := time.Now()

		// The logger is placed in the context before the handler runs so everything
		// downstream can reach it through logger.Ctx.
		ctx = logger.WithContext(ctx, log)

		resp, err := handler(ctx, req)

		writeAccessLog(ctx, info.FullMethod, time.Since(start), err)

		return resp, err
	}
}

// LoggingStream is the streaming counterpart of Logging.
func LoggingStream(log zerolog.Logger) grpc.StreamServerInterceptor {
	return func(
		srv any,
		ss grpc.ServerStream,
		info *grpc.StreamServerInfo,
		handler grpc.StreamHandler,
	) error {
		start := time.Now()

		ctx := logger.WithContext(ss.Context(), log)
		wrapped := &contextServerStream{ServerStream: ss, ctx: ctx}

		err := handler(srv, wrapped)

		writeAccessLog(ctx, info.FullMethod, time.Since(start), err)

		return err
	}
}

// writeAccessLog emits the access line, choosing the level from the resulting status code.
func writeAccessLog(ctx context.Context, method string, latency time.Duration, err error) {
	code := status.Code(err)

	event := logger.Ctx(ctx).WithLevel(levelFor(err)).
		Str(logger.FieldMethod, method).
		Str("code", code.String()).
		Dur("latency", latency)

	if peerAddr := PeerAddress(ctx); peerAddr != "" {
		event = event.Str("peer", peerAddr)
	}

	if err != nil {
		event = event.Err(err)
	}

	event.Msg("gRPC call")
}

// levelFor picks the log level from the outcome.
//
// A client error is not an operational problem: 40x-equivalent codes are logged at warn so
// they do not pollute the error rate, while an internal failure is logged at error.
func levelFor(err error) zerolog.Level {
	if err == nil {
		return zerolog.InfoLevel
	}

	switch code := status.Code(err); code {
	case codesInternal, codesUnknown, codesDataLoss, codesUnavailable:
		return zerolog.ErrorLevel
	default:
		return zerolog.WarnLevel
	}
}

// contextServerStream overrides the stream context so the handler sees the logger.
type contextServerStream struct {
	grpc.ServerStream

	ctx context.Context
}

// Context returns the enriched context instead of the transport one.
func (s *contextServerStream) Context() context.Context {
	return s.ctx
}
