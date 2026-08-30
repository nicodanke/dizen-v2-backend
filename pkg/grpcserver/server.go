// Package grpcserver assembles the gRPC server every service runs: the interceptor chain
// of 03 section 7, tracing, reflection outside production, a configurable message size
// limit and graceful shutdown (RF-5).
//
// A service does not build a *grpc.Server itself. It calls New, registers its handlers on
// the returned server and starts it; everything cross-cutting is already wired.
package grpcserver

import (
	"context"
	"errors"
	"fmt"
	"net"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/reflection"
)

// ErrShutdownForced is returned when graceful shutdown did not finish within the budget
// and the server had to be stopped hard.
var ErrShutdownForced = errors.New("graceful shutdown exceeded its timeout and was forced")

// Server wraps the gRPC server together with the listener and the shutdown policy.
type Server struct {
	cfg      Config
	server   *grpc.Server
	health   *health.Server
	listener net.Listener
}

// New builds the server and applies the whole chain. It does not listen yet: the port is
// bound by Start, so a construction error is reported before anything is exposed.
func New(cfg Config, deps Dependencies) (*Server, error) {
	cfg = cfg.withDefaults()

	unary, err := buildUnaryChain(cfg, deps)
	if err != nil {
		return nil, err
	}

	opts := []grpc.ServerOption{
		grpc.ChainUnaryInterceptor(unary...),
		grpc.ChainStreamInterceptor(buildStreamChain(cfg)...),
		grpc.MaxRecvMsgSize(cfg.MaxRecvMsgSize),

		// Tracing is a StatsHandler, not an interceptor: that is the only way the span
		// covers the whole call, including the interceptors themselves (03 section 7,
		// item 2).
		grpc.StatsHandler(otelgrpc.NewServerHandler()),

		// A client that goes quiet must not hold a connection forever, and one that pings
		// too aggressively must not be able to use it as a denial of service.
		grpc.KeepaliveParams(keepalive.ServerParameters{
			MaxConnectionIdle: 5 * time.Minute,
			Time:              2 * time.Minute,
			Timeout:           20 * time.Second,
		}),
		grpc.KeepaliveEnforcementPolicy(keepalive.EnforcementPolicy{
			MinTime:             30 * time.Second,
			PermitWithoutStream: true,
		}),
	}

	server := grpc.NewServer(opts...)

	// The standard health service backs the container healthcheck and the gRPC probe.
	healthServer := health.NewServer()
	grpc_health_v1.RegisterHealthServer(server, healthServer)

	// Reflection lets grpcurl and Yaak introspect the API. It is registered only outside
	// production, where publishing the full descriptor helps an attacker map the surface.
	if !cfg.Environment.IsProduction() {
		reflection.Register(server)
	}

	return &Server{
		cfg:    cfg,
		server: server,
		health: healthServer,
	}, nil
}

// Registrar exposes the underlying server so a service can register its handlers.
func (s *Server) Registrar() grpc.ServiceRegistrar {
	return s.server
}

// Health exposes the health service so the service can flip itself to NOT_SERVING while it
// drains.
func (s *Server) Health() *health.Server {
	return s.health
}

// Address returns the address actually bound. After Start with port 0 it carries the port
// the kernel picked, which is what the tests use.
func (s *Server) Address() string {
	if s.listener == nil {
		return s.cfg.Address
	}

	return s.listener.Addr().String()
}

// Listen binds the port without serving yet. Splitting it from Serve is what lets a test
// learn the assigned port before any client dials it.
//
// The context is honored while the socket is being bound, so a startup that is already
// being canceled does not leave a listener behind.
func (s *Server) Listen(ctx context.Context) error {
	var lc net.ListenConfig

	listener, err := lc.Listen(ctx, "tcp", s.cfg.Address)
	if err != nil {
		return fmt.Errorf("listening on %s: %w", s.cfg.Address, err)
	}

	s.listener = listener

	return nil
}

// Serve blocks serving requests. It returns nil on a clean shutdown, so main.go can treat
// any non-nil error as fatal. It binds the port first if Listen was not called.
func (s *Server) Serve(ctx context.Context) error {
	if s.listener == nil {
		if err := s.Listen(ctx); err != nil {
			return err
		}
	}

	s.health.SetServingStatus("", grpc_health_v1.HealthCheckResponse_SERVING)

	if err := s.server.Serve(s.listener); err != nil && !errors.Is(err, grpc.ErrServerStopped) {
		return fmt.Errorf("serving gRPC: %w", err)
	}

	return nil
}

// Shutdown drains in-flight calls and stops the server.
//
// It first flips health to NOT_SERVING so the load balancer stops sending traffic, then
// waits for GracefulStop up to ShutdownTimeout. If the timeout expires, it forces the stop:
// a deployment that never finishes is worse than a handful of cut connections.
func (s *Server) Shutdown(ctx context.Context) error {
	s.health.SetServingStatus("", grpc_health_v1.HealthCheckResponse_NOT_SERVING)

	stopped := make(chan struct{})

	go func() {
		s.server.GracefulStop()
		close(stopped)
	}()

	timeout := time.NewTimer(s.cfg.ShutdownTimeout)
	defer timeout.Stop()

	select {
	case <-stopped:
		return nil

	case <-timeout.C:
		s.server.Stop()

		return fmt.Errorf("%w after %s", ErrShutdownForced, s.cfg.ShutdownTimeout)

	case <-ctx.Done():
		s.server.Stop()

		return ctx.Err()
	}
}
