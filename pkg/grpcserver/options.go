package grpcserver

import (
	"time"

	"github.com/rs/zerolog"
	"google.golang.org/grpc"

	"github.com/nicodanke/dizen-v2-backend/pkg/config"
	"github.com/nicodanke/dizen-v2-backend/pkg/grpcserver/interceptor"
	"github.com/nicodanke/dizen-v2-backend/pkg/observability/metrics"
)

// Default values applied when Config leaves them at zero.
const (
	// DefaultShutdownTimeout is the 15s RF-5 fixes for graceful shutdown.
	DefaultShutdownTimeout = 15 * time.Second
	// DefaultMaxRecvMsgSize is 4 MiB, the same default the gRPC runtime uses.
	DefaultMaxRecvMsgSize = 4 * 1024 * 1024
)

// Config describes the server. Everything the chain needs is passed here explicitly:
// there is no package-level state and no hidden default.
type Config struct {
	// Address is the listen address, such as ":9090".
	Address string

	// Environment gates reflection: it is only registered outside production, where
	// exposing the full service descriptor is a reconnaissance aid.
	Environment config.Environment

	// Logger is the root logger the logging interceptor injects into every request.
	Logger zerolog.Logger

	// Metrics is the registry the metrics interceptor writes to.
	Metrics *metrics.Registry

	// MaxRecvMsgSize is the largest request accepted, in bytes.
	MaxRecvMsgSize int

	// ShutdownTimeout bounds GracefulStop before the server is forced down.
	ShutdownTimeout time.Duration

	// MinClientAPIVersion is the oldest contract version accepted (RF-2c). Empty
	// disables the check.
	MinClientAPIVersion string

	// PublicMethods is the explicit allowlist of methods that skip authentication.
	// Nothing is public by default (03 section 7).
	PublicMethods *interceptor.Allowlist
}

// Dependencies carries the interceptors whose implementation arrives with a later RF.
//
// They are optional on purpose: a nil field means that link of the chain is skipped, which
// is what lets the server be assembled today and gain rate limiting, authentication and
// authorization without the assembly order ever being rewritten. The order itself lives in
// buildChain and is the one 03 section 7 fixes.
type Dependencies struct {
	// RateLimiter is the Redis token bucket. Arrives with RF-10.
	RateLimiter grpc.UnaryServerInterceptor

	// Authenticator validates the JWT and injects the principal. Arrives with RF-14.
	Authenticator grpc.UnaryServerInterceptor

	// Authorizer maps method to required permission. Arrives with PRD-14.
	Authorizer grpc.UnaryServerInterceptor

	// Extra interceptors run after the whole standard chain, closest to the handler.
	Extra []grpc.UnaryServerInterceptor
}

// withDefaults fills in the zero values so a caller only has to set what it cares about.
func (c Config) withDefaults() Config {
	if c.ShutdownTimeout <= 0 {
		c.ShutdownTimeout = DefaultShutdownTimeout
	}

	if c.MaxRecvMsgSize <= 0 {
		c.MaxRecvMsgSize = DefaultMaxRecvMsgSize
	}

	return c
}
