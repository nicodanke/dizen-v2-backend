package interceptor

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/status"

	"github.com/nicodanke/dizen-v2-backend/pkg/observability/metrics"
)

// Metrics records the counter and the latency histogram per method (03 section 7).
//
// The method label is the fully qualified name, which is a bounded set: it comes from the
// contract, not from user input, so it cannot blow up the cardinality of the series.
func Metrics(registry *metrics.Registry) grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		req any,
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (any, error) {
		finish := registry.RequestStarted(info.FullMethod)

		resp, err := handler(ctx, req)

		finish(status.Code(err))

		return resp, err
	}
}

// MetricsStream is the streaming counterpart of Metrics.
func MetricsStream(registry *metrics.Registry) grpc.StreamServerInterceptor {
	return func(
		srv any,
		ss grpc.ServerStream,
		info *grpc.StreamServerInfo,
		handler grpc.StreamHandler,
	) error {
		finish := registry.RequestStarted(info.FullMethod)

		err := handler(srv, ss)

		finish(status.Code(err))

		return err
	}
}
