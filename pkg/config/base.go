package config

import "time"

// Base is the configuration every service shares. Each service embeds it and adds its own
// fields; the loader flattens embedded structs, so the env var names stay flat.
//
// Configuration that belongs to a subsystem lives with that subsystem -- database.Config,
// cache.Config, amqp.Config -- so that a service only pulls in what it actually uses.
type Base struct {
	// ServiceName identifies the service in logs, metrics and traces.
	ServiceName string `env:"SERVICE_NAME" validate:"required"`

	// Environment gates behavior that must differ outside production.
	Environment Environment `env:"ENVIRONMENT" envDefault:"local" validate:"required,oneof=local staging production test"`

	// LogLevel is the zerolog level: trace, debug, info, warn, error, fatal, panic.
	LogLevel string `env:"LOG_LEVEL" envDefault:"info" validate:"required,oneof=trace debug info warn error fatal panic"`

	// GRPCPort is the port the gRPC server listens on.
	GRPCPort int `env:"GRPC_PORT" envDefault:"9090" validate:"required,min=1,max=65535"`

	// HTTPPort is the port serving the REST gateway, /livez, /readyz and /metrics.
	HTTPPort int `env:"HTTP_PORT" envDefault:"8080" validate:"required,min=1,max=65535"`

	// ShutdownTimeout bounds graceful shutdown. RF-5 fixes 15s as the default.
	ShutdownTimeout time.Duration `env:"SHUTDOWN_TIMEOUT" envDefault:"15s" validate:"required,min=1s"`

	// GRPCMaxRecvMsgSize is the largest request the gRPC server accepts, in bytes.
	GRPCMaxRecvMsgSize int `env:"GRPC_MAX_RECV_MSG_SIZE" envDefault:"4194304" validate:"required,min=1024"`

	// RequestTimeout is the deadline applied to inbound calls that arrive without one.
	// Hard rule 7: nothing runs unbounded.
	RequestTimeout time.Duration `env:"REQUEST_TIMEOUT" envDefault:"30s" validate:"required,min=1s"`

	// MinClientAPIVersion is the oldest contract version the service accepts. Clients
	// below it are rejected with CLIENT_TOO_OLD (RF-2c). Empty disables the check.
	MinClientAPIVersion string `env:"MIN_CLIENT_API_VERSION" envDefault:""`

	// OTLPEndpoint is the OpenTelemetry collector address. Empty disables tracing, which
	// is what unit tests and a bare `go run` want.
	OTLPEndpoint string `env:"OTLP_ENDPOINT" envDefault:""`

	// TraceSampleRatio is the fraction of traces sampled, between 0 and 1.
	TraceSampleRatio float64 `env:"TRACE_SAMPLE_RATIO" envDefault:"1.0" validate:"gte=0,lte=1"`
}

// GRPCAddress is the listen address of the gRPC server.
func (b Base) GRPCAddress() string {
	return addr(b.GRPCPort)
}

// HTTPAddress is the listen address of the HTTP server.
func (b Base) HTTPAddress() string {
	return addr(b.HTTPPort)
}

// TracingEnabled reports whether an OTLP collector was configured.
func (b Base) TracingEnabled() bool {
	return b.OTLPEndpoint != ""
}
