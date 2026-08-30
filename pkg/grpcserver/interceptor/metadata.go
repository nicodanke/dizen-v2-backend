// Package interceptor holds the gRPC server interceptor chain described in 03 section 7.
//
// Each interceptor lives in its own file and is built by a constructor that takes its
// dependencies explicitly. The order they run in is not decided here: it is fixed once, in
// grpcserver.buildChain.
package interceptor

import (
	"context"

	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
)

// Metadata keys the client is required to send (03 section 1). They are constants because
// they are part of the contract: the mobile app and the dashboard send exactly these.
const (
	// MDAuthorization carries "Bearer <jwt>".
	MDAuthorization = "authorization"
	// MDDeviceID identifies the installation.
	MDDeviceID = "x-device-id"
	// MDAppVersion is the version of the app making the call.
	MDAppVersion = "x-app-version"
	// MDAPIVersion is the contract version the client was built against (RF-2c).
	MDAPIVersion = "x-api-version"
	// MDPlatform is ios, android or web.
	MDPlatform = "x-platform"
	// MDAcceptLanguage selects the content language.
	MDAcceptLanguage = "accept-language"
)

// First returns the first value of a metadata key, or an empty string when absent.
// gRPC metadata is multi-valued; every key in our contract is single-valued, so taking the
// first is the correct reading and not a shortcut.
func First(md metadata.MD, key string) string {
	values := md.Get(key)
	if len(values) == 0 {
		return ""
	}

	return values[0]
}

// FromContext returns the incoming metadata, or an empty set when the call carries none.
func FromContext(ctx context.Context) metadata.MD {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return metadata.MD{}
	}

	return md
}

// PeerAddress returns the address of the caller, used by the rate limiter and by the
// logger. It is empty when the transport does not expose one, as in an in-process test.
func PeerAddress(ctx context.Context) string {
	p, ok := peer.FromContext(ctx)
	if !ok || p.Addr == nil {
		return ""
	}

	return p.Addr.String()
}
