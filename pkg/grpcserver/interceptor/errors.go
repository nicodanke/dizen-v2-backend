package interceptor

import (
	"context"

	"google.golang.org/grpc"

	dizenerrors "github.com/nicodanke/dizen-v2-backend/pkg/errors"
)

// Errors normalizes whatever a handler returns into the gRPC status the client receives
// (RF-13).
//
// It is the innermost interceptor of the standard chain, wrapping the handler directly.
// The position matters in both directions: everything the handler returns passes through
// it, and because errors propagate outward, the logging and metrics interceptors above see
// the already mapped code rather than the raw error.
//
// A handler can therefore return a plain error and be certain of two things: the client
// learns nothing internal, and the failure is in the log with its trace_id.
func Errors() grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		req any,
		_ *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (any, error) {
		resp, err := handler(ctx, req)
		if err != nil {
			return nil, dizenerrors.ToStatus(ctx, err)
		}

		return resp, nil
	}
}

// ErrorsStream is the streaming counterpart.
func ErrorsStream() grpc.StreamServerInterceptor {
	return func(
		srv any,
		ss grpc.ServerStream,
		_ *grpc.StreamServerInfo,
		handler grpc.StreamHandler,
	) error {
		if err := handler(srv, ss); err != nil {
			return dizenerrors.ToStatus(ss.Context(), err)
		}

		return nil
	}
}
