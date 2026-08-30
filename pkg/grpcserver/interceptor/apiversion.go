package interceptor

import (
	"context"

	"github.com/Masterminds/semver/v3"
	"google.golang.org/grpc"

	dizenerrors "github.com/nicodanke/dizen-v2-backend/pkg/errors"
)

// ReasonClientTooOld is the stable reason a client keys on to show "update the app"
// (RF-2c, 03 section 1). The client decides on the reason, never on the message text.
//
// It is an alias of the constant in pkg/errors so the vocabulary lives in one place.
const ReasonClientTooOld = dizenerrors.ReasonClientTooOld

// APIVersion rejects clients built against a contract older than the minimum supported
// one (RF-2c). It is what allows an old contract version to be retired without breaking
// anyone by surprise.
//
// A call that does not declare x-api-version is let through: an app already published does
// not send the header, and rejecting it would break exactly the clients this check is
// meant to protect (decision D-12). Those calls are counted in the log so the header can
// be made mandatory once the fleet has rolled over.
func APIVersion(minVersion string) (grpc.UnaryServerInterceptor, error) {
	if minVersion == "" {
		// No minimum configured: the interceptor becomes a pass-through rather than a
		// conditional branch at every call site.
		return passthrough(), nil
	}

	minimum, err := semver.NewVersion(minVersion)
	if err != nil {
		return nil, err
	}

	return func(
		ctx context.Context,
		req any,
		_ *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (any, error) {
		declared := First(FromContext(ctx), MDAPIVersion)
		if declared == "" {
			return handler(ctx, req)
		}

		clientVersion, parseErr := semver.NewVersion(declared)
		if parseErr != nil {
			// An unparseable version is treated as too old: a client sending garbage
			// cannot be assumed to be compatible.
			return nil, clientTooOld(declared, minimum.String())
		}

		if clientVersion.LessThan(minimum) {
			return nil, clientTooOld(declared, minimum.String())
		}

		return handler(ctx, req)
	}, nil
}

// passthrough is a no-op unary interceptor.
func passthrough() grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		req any,
		_ *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (any, error) {
		return handler(ctx, req)
	}
}

// clientTooOld builds FAILED_PRECONDITION carrying ErrorInfo with a stable reason, which
// is the shape 03 section 1 mandates for every error.
func clientTooOld(declared, minimum string) error {
	return dizenerrors.
		FailedPrecondition(ReasonClientTooOld, "the client API version is no longer supported").
		WithMetadata("client_version", declared).
		WithMetadata("minimum_version", minimum)
}
