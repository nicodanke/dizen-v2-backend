package httpserver_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/nicodanke/dizen-v2-backend/pkg/config"
	identityv1 "github.com/nicodanke/dizen-v2-backend/pkg/genproto/dizen/identity/v1"
	"github.com/nicodanke/dizen-v2-backend/pkg/grpcserver"
	"github.com/nicodanke/dizen-v2-backend/pkg/health"
	"github.com/nicodanke/dizen-v2-backend/pkg/httpserver"
	"github.com/nicodanke/dizen-v2-backend/pkg/logger"
	"github.com/nicodanke/dizen-v2-backend/pkg/observability/metrics"
)

// healthService is the reference RPC of RF-19, enough to exercise the gateway.
type healthService struct {
	identityv1.UnimplementedHealthServiceServer
}

func (healthService) HealthPing(
	_ context.Context,
	_ *identityv1.HealthPingRequest,
) (*identityv1.HealthPingResponse, error) {
	return &identityv1.HealthPingResponse{
		Service: "identity",
		Version: "v0.1.0",
		Commit:  "abc1234",
	}, nil
}

// stack is a gRPC server and an HTTP server wired exactly as a real service wires them.
type stack struct {
	httpAddr string
	registry *health.Registry
}

// newStack builds the whole transport layer on ephemeral ports.
func newStack(t *testing.T, checks ...health.Check) *stack {
	t.Helper()

	metricsRegistry := metrics.NewRegistry("identity")
	healthRegistry := health.NewRegistry("identity", "v0.1.0")

	for _, check := range checks {
		healthRegistry.Register(check)
	}

	grpcSrv, err := grpcserver.New(grpcserver.Config{
		Address:     "127.0.0.1:0",
		Environment: config.EnvTest,
		Logger:      logger.Nop(),
		Metrics:     metricsRegistry,
	}, grpcserver.Dependencies{})
	if err != nil {
		t.Fatalf("grpcserver.New: %v", err)
	}

	identityv1.RegisterHealthServiceServer(grpcSrv.Registrar(), healthService{})

	if err := grpcSrv.Listen(t.Context()); err != nil {
		t.Fatalf("gRPC Listen: %v", err)
	}

	go func() { _ = grpcSrv.Serve(context.Background()) }()

	// The gateway reaches the gRPC server over loopback, so REST goes through the same
	// interceptor chain as gRPC.
	conn, err := httpserver.DialGRPC(grpcSrv.Address())
	if err != nil {
		t.Fatalf("DialGRPC: %v", err)
	}

	mux := httpserver.NewGatewayMux()

	err = httpserver.RegisterGateways(context.Background(), mux, conn,
		identityv1.RegisterHealthServiceHandler,
	)
	if err != nil {
		t.Fatalf("RegisterGateways: %v", err)
	}

	httpSrv, err := httpserver.New(httpserver.Config{
		Address:     "127.0.0.1:0",
		ServiceName: "identity",
		Version:     "v0.1.0",
		Logger:      logger.Nop(),
		Gateway:     mux,
		Health:      healthRegistry,
		Metrics:     metricsRegistry.Prometheus(),
	})
	if err != nil {
		t.Fatalf("httpserver.New: %v", err)
	}

	if err := httpSrv.Listen(t.Context()); err != nil {
		t.Fatalf("HTTP Listen: %v", err)
	}

	go func() { _ = httpSrv.Serve(context.Background()) }()

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		_ = httpSrv.Shutdown(ctx)
		_ = conn.Close()
		_ = grpcSrv.Shutdown(ctx)
	})

	return &stack{httpAddr: httpSrv.Address(), registry: healthRegistry}
}

// get performs a GET against the stack.
func (s *stack) get(t *testing.T, path string) (int, string) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+s.httpAddr+path, nil)
	if err != nil {
		t.Fatalf("building the request: %v", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading the body: %v", err)
	}

	return resp.StatusCode, string(body)
}

func TestLivezAnswers200(t *testing.T) {
	t.Parallel()

	s := newStack(t)

	code, body := s.get(t, httpserver.PathLiveness)

	if code != http.StatusOK {
		t.Errorf("status = %d, want 200. Body: %s", code, body)
	}

	if !strings.Contains(body, `"service":"identity"`) {
		t.Errorf("the body does not identify the service: %s", body)
	}
}

// This is acceptance criterion 1 of PRD-00, at the level a unit test can reach: the service
// answers 200 on /livez and /readyz. The full compose version arrives with RF-15.
func TestReadyzAnswers200WhenEveryDependencyIsUp(t *testing.T) {
	t.Parallel()

	up := func(context.Context) error { return nil }

	s := newStack(t,
		health.Check{Name: "postgres", Probe: up, Critical: true},
		health.Check{Name: "redis", Probe: up, Critical: true},
		health.Check{Name: "amqp", Probe: up, Critical: true},
	)

	code, body := s.get(t, httpserver.PathReadiness)

	if code != http.StatusOK {
		t.Errorf("status = %d, want 200. Body: %s", code, body)
	}
}

func TestReadyzAnswers503AndNamesTheFailingDependency(t *testing.T) {
	t.Parallel()

	up := func(context.Context) error { return nil }
	down := func(context.Context) error { return errors.New("dial tcp 127.0.0.1:5432: connect: connection refused") }

	s := newStack(t,
		health.Check{Name: "postgres", Probe: down, Critical: true},
		health.Check{Name: "redis", Probe: up, Critical: true},
	)

	code, body := s.get(t, httpserver.PathReadiness)

	if code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503. Body: %s", code, body)
	}

	if !strings.Contains(body, "connection refused") {
		t.Errorf("the response does not say what is failing: %s", body)
	}

	if !strings.Contains(body, `"name":"postgres"`) {
		t.Errorf("the response does not name the failing dependency: %s", body)
	}
}

func TestMetricsIsScrapeable(t *testing.T) {
	t.Parallel()

	s := newStack(t)

	code, body := s.get(t, httpserver.PathMetrics)

	if code != http.StatusOK {
		t.Errorf("status = %d, want 200", code)
	}

	// The Go runtime collector is always registered, so its metrics are proof the
	// endpoint is really serving the registry.
	if !strings.Contains(body, "go_goroutines") {
		t.Errorf("the exposition does not look like Prometheus:\n%s", body[:min(len(body), 300)])
	}
}

// The core of RF-6: the same RPC answers over gRPC and over REST, from one contract.
func TestTheGatewayExposesTheRPCOverREST(t *testing.T) {
	t.Parallel()

	s := newStack(t)

	code, body := s.get(t, "/v1/identity/health")

	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200. Body: %s", code, body)
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		t.Fatalf("the response is not JSON: %v. Body: %s", err, body)
	}

	if payload["service"] != "identity" {
		t.Errorf("service = %v", payload["service"])
	}

	if payload["commit"] != "abc1234" {
		t.Errorf("commit = %v", payload["commit"])
	}

	// The dashboard consumes camelCase, which is what the OpenAPI contract describes.
	if _, ok := payload["serverTime"]; !ok {
		t.Errorf("serverTime is missing: EmitUnpopulated is not applying. Body: %s", body)
	}
}

func TestAnUnknownRouteIs404(t *testing.T) {
	t.Parallel()

	s := newStack(t)

	code, _ := s.get(t, "/v1/does-not-exist")

	if code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", code)
	}
}

// A service with no public API -- mail-dispatcher -- must still serve the operational
// routes (decision D-6).
func TestAServerWithoutAGatewayStillServesTheOperationalRoutes(t *testing.T) {
	t.Parallel()

	registry := health.NewRegistry("mail-dispatcher", "v0.1.0")

	srv, err := httpserver.New(httpserver.Config{
		Address:     "127.0.0.1:0",
		ServiceName: "mail-dispatcher",
		Version:     "v0.1.0",
		Logger:      logger.Nop(),
		Gateway:     nil,
		Health:      registry,
		Metrics:     prometheus.NewRegistry(),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if err := srv.Listen(t.Context()); err != nil {
		t.Fatalf("Listen: %v", err)
	}

	go func() { _ = srv.Serve(context.Background()) }()

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		_ = srv.Shutdown(ctx)
	})

	s := &stack{httpAddr: srv.Address()}

	if code, _ := s.get(t, httpserver.PathLiveness); code != http.StatusOK {
		t.Errorf("/livez = %d, want 200", code)
	}

	if code, _ := s.get(t, httpserver.PathReadiness); code != http.StatusOK {
		t.Errorf("/readyz = %d, want 200", code)
	}
}

func TestNewRequiresAHealthRegistry(t *testing.T) {
	t.Parallel()

	_, err := httpserver.New(httpserver.Config{Address: "127.0.0.1:0"})
	if err == nil {
		t.Fatal("a server was built without a health registry")
	}
}
