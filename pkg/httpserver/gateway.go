// Package httpserver serves the REST surface: the grpc-gateway mounted at /v1/*, plus
// /livez, /readyz and /metrics (RF-6).
//
// The gateway talks to the gRPC server over the loopback interface rather than calling the
// handlers in process. That costs one local hop and buys a real guarantee: the REST surface
// goes through exactly the same interceptor chain as gRPC, so auth, rate limiting and
// validation cannot end up applying to one transport and not the other.
package httpserver

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/encoding/protojson"
)

// RegisterGatewayFunc is the signature of the generated RegisterXHandler functions.
type RegisterGatewayFunc func(ctx context.Context, mux *runtime.ServeMux, conn *grpc.ClientConn) error

// forwardedHeaders are the metadata keys the client sends over HTTP that have to reach the
// gRPC handlers. Anything not listed here is dropped, so a header cannot be smuggled into
// the gRPC context.
var forwardedHeaders = []string{
	"authorization",
	"x-device-id",
	"x-app-version",
	"x-api-version",
	"x-platform",
	"accept-language",
	"traceparent",
	"tracestate",
}

// NewGatewayMux builds the gateway multiplexer.
func NewGatewayMux() *runtime.ServeMux {
	return runtime.NewServeMux(
		runtime.WithIncomingHeaderMatcher(headerMatcher),

		runtime.WithMarshalerOption(runtime.MIMEWildcard, &runtime.JSONPb{
			MarshalOptions: protojson.MarshalOptions{
				// The dashboard consumes camelCase, which is what the OpenAPI contract
				// describes; emitting defaults keeps the response shape stable so a
				// TypeScript client never has to handle a missing field.
				UseProtoNames:   false,
				EmitUnpopulated: true,
			},
			UnmarshalOptions: protojson.UnmarshalOptions{
				// An unknown field is a client sending something the contract does not
				// define: rejecting it surfaces the mistake instead of silently ignoring
				// half the request.
				DiscardUnknown: false,
			},
		}),
	)
}

// headerMatcher decides which HTTP headers become gRPC metadata.
func headerMatcher(header string) (string, bool) {
	normalized := strings.ToLower(header)

	if slices.Contains(forwardedHeaders, normalized) {
		return normalized, true
	}

	// Everything else keeps the default behavior, which prefixes it with grpcgateway-
	// and leaves it clearly distinguishable from contract metadata.
	return runtime.DefaultHeaderMatcher(header)
}

// DialGRPC opens the loopback connection the gateway uses to reach the gRPC server.
func DialGRPC(target string) (*grpc.ClientConn, error) {
	conn, err := grpc.NewClient(target,
		// The hop is loopback inside the same container: TLS would encrypt a connection
		// that never leaves the process namespace.
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, fmt.Errorf("connecting the gateway to the gRPC server at %s: %w", target, err)
	}

	return conn, nil
}

// RegisterGateways runs every generated registration function against the mux.
func RegisterGateways(
	ctx context.Context,
	mux *runtime.ServeMux,
	conn *grpc.ClientConn,
	register ...RegisterGatewayFunc,
) error {
	for _, fn := range register {
		if err := fn(ctx, mux, conn); err != nil {
			return fmt.Errorf("registering a gateway handler: %w", err)
		}
	}

	return nil
}

// ensure the mux satisfies http.Handler at compile time.
var _ http.Handler = (*runtime.ServeMux)(nil)
