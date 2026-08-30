package httpserver

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/zerolog"

	"github.com/nicodanke/dizen-v2-backend/pkg/health"
)

// Route prefixes. /v1 is the REST contract; the other three are operational and are not
// routed through Traefik to the public internet.
const (
	// PathAPI is where the gateway is mounted.
	PathAPI = "/v1/"
	// PathLiveness reports that the process is alive.
	PathLiveness = "/livez"
	// PathReadiness reports that the service can take traffic.
	PathReadiness = "/readyz"
	// PathMetrics is scraped by Prometheus.
	PathMetrics = "/metrics"
)

// Timeouts applied to every HTTP connection. They exist so a slow or malicious client
// cannot pin a connection open indefinitely.
const (
	defaultReadHeaderTimeout = 10 * time.Second
	defaultReadTimeout       = 30 * time.Second
	defaultWriteTimeout      = 60 * time.Second
	defaultIdleTimeout       = 120 * time.Second
)

// ErrHealthRegistryRequired is returned when New is called without a health registry.
// Without it there is no /readyz, and a service that cannot report readiness must not
// start rather than start silently unmonitored.
var ErrHealthRegistryRequired = errors.New("httpserver: a health registry is required")

// Config describes the HTTP server.
type Config struct {
	// Address is the listen address, such as ":8080".
	Address string

	// ServiceName and Version are reported by /livez and /readyz.
	ServiceName string
	Version     string

	// Logger records startup and shutdown.
	Logger zerolog.Logger

	// Gateway is the grpc-gateway mux mounted at /v1/. It may be nil for a service with
	// no public API, such as mail-dispatcher, which still serves the operational routes.
	Gateway *runtime.ServeMux

	// Health is the readiness registry. Required.
	Health *health.Registry

	// Metrics is the Prometheus registry served at /metrics. It may be nil.
	Metrics *prometheus.Registry

	// Extra mounts service-specific handlers by path, such as the JWKS that identity
	// publishes. It exists so a service can add a route without this package having to
	// know what the route is for.
	Extra map[string]http.Handler

	// ShutdownTimeout bounds graceful shutdown.
	ShutdownTimeout time.Duration
}

// Server is the HTTP server together with its listener.
type Server struct {
	cfg      Config
	server   *http.Server
	listener net.Listener
}

// New builds the server and its routes.
func New(cfg Config) (*Server, error) {
	if cfg.Health == nil {
		return nil, ErrHealthRegistryRequired
	}

	if cfg.ShutdownTimeout <= 0 {
		cfg.ShutdownTimeout = 15 * time.Second
	}

	mux := http.NewServeMux()

	mux.HandleFunc(PathLiveness, health.LivenessHandler(cfg.ServiceName, cfg.Version))
	mux.HandleFunc(PathReadiness, cfg.Health.ReadinessHandler())

	if cfg.Metrics != nil {
		mux.Handle(PathMetrics, promhttp.HandlerFor(cfg.Metrics, promhttp.HandlerOpts{
			// A failing collector must not take the scrape endpoint down with it.
			ErrorHandling: promhttp.ContinueOnError,
		}))
	}

	if cfg.Gateway != nil {
		mux.Handle(PathAPI, cfg.Gateway)
	}

	for path, handler := range cfg.Extra {
		mux.Handle(path, handler)
	}

	return &Server{
		cfg: cfg,
		server: &http.Server{
			Addr:              cfg.Address,
			Handler:           mux,
			ReadHeaderTimeout: defaultReadHeaderTimeout,
			ReadTimeout:       defaultReadTimeout,
			WriteTimeout:      defaultWriteTimeout,
			IdleTimeout:       defaultIdleTimeout,
		},
	}, nil
}

// Address returns the address actually bound.
func (s *Server) Address() string {
	if s.listener == nil {
		return s.cfg.Address
	}

	return s.listener.Addr().String()
}

// Listen binds the port without serving yet, so a test can learn the assigned port.
func (s *Server) Listen(ctx context.Context) error {
	var lc net.ListenConfig

	listener, err := lc.Listen(ctx, "tcp", s.cfg.Address)
	if err != nil {
		return fmt.Errorf("listening on %s: %w", s.cfg.Address, err)
	}

	s.listener = listener

	return nil
}

// Serve blocks serving requests, returning nil on a clean shutdown. It binds the port
// first if Listen was not called.
func (s *Server) Serve(ctx context.Context) error {
	if s.listener == nil {
		if err := s.Listen(ctx); err != nil {
			return err
		}
	}

	if err := s.server.Serve(s.listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("serving HTTP: %w", err)
	}

	return nil
}

// Shutdown drains in-flight requests within the configured timeout.
func (s *Server) Shutdown(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, s.cfg.ShutdownTimeout)
	defer cancel()

	if err := s.server.Shutdown(ctx); err != nil {
		return fmt.Errorf("shutting down HTTP: %w", err)
	}

	return nil
}
