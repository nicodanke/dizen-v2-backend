package grpcserver

import (
	"fmt"

	"google.golang.org/grpc"

	"github.com/nicodanke/dizen-v2-backend/pkg/grpcserver/interceptor"
)

// buildUnaryChain assembles the interceptors in the order 03 section 7 fixes.
//
//  1. recovery    -- turns a panic into INTERNAL without leaking the stack
//  2. tracing     -- otelgrpc, installed as a StatsHandler, not as an interceptor
//  3. logging     -- one line per call, with the logger injected into the context
//  4. metrics     -- counter and latency histogram
//     api_version -- rejects clients below the minimum contract (RF-2c)
//  5. rate_limit  -- Redis token bucket                            (RF-10)
//  6. auth        -- validates the JWT and injects the principal   (RF-14)
//  7. authz       -- method to required permission                 (PRD-14)
//  8. validate    -- protovalidate over the request
//
// Order is not a matter of taste. recovery has to be first so it also covers a panic in
// any interceptor below it; logging before metrics so a rejected call is still logged;
// api_version before the expensive links so an obsolete client is turned away early; and
// validate last so an unauthenticated caller cannot probe request shapes through
// validation errors.
func buildUnaryChain(cfg Config, deps Dependencies) ([]grpc.UnaryServerInterceptor, error) {
	chain := []grpc.UnaryServerInterceptor{
		interceptor.Recovery(cfg.Logger),
		interceptor.Logging(cfg.Logger),
	}

	if cfg.Metrics != nil {
		chain = append(chain, interceptor.Metrics(cfg.Metrics))
	}

	apiVersion, err := interceptor.APIVersion(cfg.MinClientAPIVersion)
	if err != nil {
		return nil, fmt.Errorf("MIN_CLIENT_API_VERSION %q is not valid semver: %w", cfg.MinClientAPIVersion, err)
	}

	chain = append(chain, apiVersion)

	// The three links below arrive with their own RF. A nil dependency is skipped rather
	// than replaced by a permissive stub: a chain that silently authorizes everything is
	// worse than one that visibly lacks the link.
	if deps.RateLimiter != nil {
		chain = append(chain, deps.RateLimiter)
	}

	if deps.Authenticator != nil {
		chain = append(chain, deps.Authenticator)
	}

	if deps.Authorizer != nil {
		chain = append(chain, deps.Authorizer)
	}

	validator, err := interceptor.NewValidator()
	if err != nil {
		return nil, err
	}

	chain = append(chain, interceptor.Validate(validator))
	chain = append(chain, interceptor.Errors())
	chain = append(chain, deps.Extra...)

	return chain, nil
}

// buildStreamChain assembles the streaming counterpart. Only the links that make sense on
// a stream are included; the rest arrive with the RFs that introduce streaming RPCs.
func buildStreamChain(cfg Config) []grpc.StreamServerInterceptor {
	chain := []grpc.StreamServerInterceptor{
		interceptor.RecoveryStream(cfg.Logger),
		interceptor.LoggingStream(cfg.Logger),
	}

	if cfg.Metrics != nil {
		chain = append(chain, interceptor.MetricsStream(cfg.Metrics))
	}

	chain = append(chain, interceptor.ErrorsStream())

	return chain
}
