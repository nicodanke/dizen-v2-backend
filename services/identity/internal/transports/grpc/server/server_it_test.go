//go:build integration

// End-to-end test of the reference service (RF-19).
//
// It stands up what a real deployment stands up -- a migrated Postgres, the gRPC server with
// the full interceptor chain, and the REST gateway on top -- and exercises the one RPC the
// service has, over both transports. It is the proof that the template works end to end, and
// the thing the other four services are copied from.

package server_test

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	pkgconfig "github.com/nicodanke/dizen-v2-backend/pkg/config"
	"github.com/nicodanke/dizen-v2-backend/pkg/database"
	dizenerrors "github.com/nicodanke/dizen-v2-backend/pkg/errors"
	identityv1 "github.com/nicodanke/dizen-v2-backend/pkg/genproto/dizen/identity/v1"
	"github.com/nicodanke/dizen-v2-backend/pkg/grpcserver"
	"github.com/nicodanke/dizen-v2-backend/pkg/grpcserver/interceptor"
	"github.com/nicodanke/dizen-v2-backend/pkg/health"
	"github.com/nicodanke/dizen-v2-backend/pkg/httpserver"
	"github.com/nicodanke/dizen-v2-backend/pkg/logger"
	"github.com/nicodanke/dizen-v2-backend/pkg/observability/metrics"
	"github.com/nicodanke/dizen-v2-backend/pkg/testutils"
	"github.com/nicodanke/dizen-v2-backend/services/identity/internal/db/migrations"
	"github.com/nicodanke/dizen-v2-backend/services/identity/internal/repository"
	"github.com/nicodanke/dizen-v2-backend/services/identity/internal/service"
	"github.com/nicodanke/dizen-v2-backend/services/identity/internal/transports/grpc/server/handler"
)

func TestMain(m *testing.M) {
	if !testutils.DockerAvailable() {
		os.Exit(0)
	}

	os.Exit(m.Run())
}

// stack is the service as a deployment runs it.
type stack struct {
	grpcClient identityv1.HealthServiceClient
	httpAddr   string
	health     *health.Registry
	repo       *repository.Repository
}

// newStack wires the service the same way main.go does, minus the parts a test cannot use:
// signal handling and the process lifetime.
func newStack(t *testing.T, opts ...func(*grpcserver.Config)) *stack {
	t.Helper()
	testutils.SkipIfNoDocker(t)

	pg := testutils.SetupPostgres(t, testutils.WithDatabase("identity_db"))

	// Migrations at startup, exactly as the container does (RF-7).
	if err := database.Migrate(pg.URL, migrations.FS, migrations.Path, logger.Nop()); err != nil {
		t.Fatalf("migrating: %v", err)
	}

	db, err := database.Connect(t.Context(), database.Config{URL: pg.URL}, logger.Nop())
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}

	t.Cleanup(db.Close)

	healthRegistry := health.NewRegistry("identity", "v0.1.0")
	healthRegistry.Register(db.HealthCheck())

	metricsRegistry := metrics.NewRegistry("identity")

	cfg := grpcserver.Config{
		Address:     "127.0.0.1:0",
		Environment: pkgconfig.EnvTest,
		Logger:      logger.Nop(),
		Metrics:     metricsRegistry,
		PublicMethods: interceptor.NewAllowlist(append(
			interceptor.HealthMethods(),
			identityv1.HealthService_HealthPing_FullMethodName,
		)...),
	}

	for _, opt := range opts {
		opt(&cfg)
	}

	grpcSrv, err := grpcserver.New(cfg, grpcserver.Dependencies{})
	if err != nil {
		t.Fatalf("grpcserver.New: %v", err)
	}

	identityv1.RegisterHealthServiceServer(
		grpcSrv.Registrar(),
		handler.NewHealthHandler(service.NewHealthService("identity")),
	)

	if err := grpcSrv.Listen(t.Context()); err != nil {
		t.Fatalf("gRPC Listen: %v", err)
	}

	go func() { _ = grpcSrv.Serve(context.Background()) }()

	conn, err := httpserver.DialGRPC(grpcSrv.Address())
	if err != nil {
		t.Fatalf("DialGRPC: %v", err)
	}

	mux := httpserver.NewGatewayMux()

	if err := httpserver.RegisterGateways(t.Context(), mux, conn, identityv1.RegisterHealthServiceHandler); err != nil {
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

	client, err := grpc.NewClient(grpcSrv.Address(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("dialing gRPC: %v", err)
	}

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		_ = client.Close()
		_ = httpSrv.Shutdown(ctx)
		_ = conn.Close()
		_ = grpcSrv.Shutdown(ctx)
	})

	return &stack{
		grpcClient: identityv1.NewHealthServiceClient(client),
		httpAddr:   httpSrv.Address(),
		health:     healthRegistry,
		repo:       repository.New(db.Pool()),
	}
}

// get performs a GET against the HTTP server.
func (s *stack) get(t *testing.T, path string) (int, string) {
	t.Helper()

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
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

// The reference RPC of RF-19, over the transport the Flutter app uses.
func TestHealthPingOverGRPC(t *testing.T) {
	s := newStack(t)

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	resp, err := s.grpcClient.HealthPing(ctx, &identityv1.HealthPingRequest{})
	if err != nil {
		t.Fatalf("HealthPing: %v", err)
	}

	if resp.GetService() != "identity" {
		t.Errorf("service = %q", resp.GetService())
	}

	if resp.GetVersion() == "" {
		t.Error("the response carries no version")
	}

	if resp.GetServerTime() == nil {
		t.Error("the response carries no server time")
	}

	// The time has to be roughly now: a zero or wildly wrong clock would make every
	// timestamp downstream suspect.
	if delta := time.Since(resp.GetServerTime().AsTime()); delta > time.Minute || delta < -time.Minute {
		t.Errorf("server time is off by %s", delta)
	}
}

// The same RPC over the transport the dashboard uses. One contract, two surfaces.
func TestHealthPingOverREST(t *testing.T) {
	s := newStack(t)

	code, body := s.get(t, "/v1/identity/health")

	if code != http.StatusOK {
		t.Fatalf("status = %d. Body: %s", code, body)
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		t.Fatalf("the response is not JSON: %v. Body: %s", err, body)
	}

	if payload["service"] != "identity" {
		t.Errorf("service = %v", payload["service"])
	}

	// camelCase, which is what the OpenAPI contract describes and what the TypeScript
	// client expects.
	if _, ok := payload["serverTime"]; !ok {
		t.Errorf("serverTime is missing: the gateway is not emitting the contract shape. Body: %s", body)
	}
}

// Acceptance criterion 1 at the service level: /livez and /readyz answer 200 with a live
// database behind them.
func TestLivezAndReadyzAnswer200(t *testing.T) {
	s := newStack(t)

	if code, body := s.get(t, httpserver.PathLiveness); code != http.StatusOK {
		t.Errorf("/livez = %d. Body: %s", code, body)
	}

	code, body := s.get(t, httpserver.PathReadiness)
	if code != http.StatusOK {
		t.Fatalf("/readyz = %d. Body: %s", code, body)
	}

	var report health.Report
	if err := json.Unmarshal([]byte(body), &report); err != nil {
		t.Fatalf("the readiness report is not JSON: %v", err)
	}

	if !report.Ready() {
		t.Errorf("the report is not ready: %s", body)
	}

	// The database has to be among the checks, and critical.
	var found bool

	for _, check := range report.Checks {
		if check.Name == database.CheckName {
			found = true

			if !check.Critical {
				t.Error("the database check is not critical")
			}

			if check.Status != health.StatusUp {
				t.Errorf("the database is reported as %q", check.Status)
			}
		}
	}

	if !found {
		t.Errorf("the database is not among the readiness checks: %s", body)
	}
}

// A dependency that goes away has to show up as 503 naming it, which is how an operator tells
// "the service is down" from "its database is".
func TestReadyzTurns503WhenADependencyFails(t *testing.T) {
	s := newStack(t)

	s.health.Register(health.Check{
		Name:     "a-broken-dependency",
		Critical: true,
		Probe: func(context.Context) error {
			return net.ErrClosed
		},
	})

	code, body := s.get(t, httpserver.PathReadiness)

	if code != http.StatusServiceUnavailable {
		t.Fatalf("/readyz = %d, want 503. Body: %s", code, body)
	}

	if !strings.Contains(body, "a-broken-dependency") {
		t.Errorf("the response does not name the failing dependency: %s", body)
	}
}

func TestMetricsRecordTheRealCall(t *testing.T) {
	s := newStack(t)

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	if _, err := s.grpcClient.HealthPing(ctx, &identityv1.HealthPingRequest{}); err != nil {
		t.Fatalf("HealthPing: %v", err)
	}

	_, body := s.get(t, httpserver.PathMetrics)

	if !strings.Contains(body, `dizen_grpc_requests_total{code="OK",method="/dizen.identity.v1.HealthService/HealthPing"`) {
		t.Errorf("the call was not counted:\n%s", body)
	}
}

// RF-2c through the whole stack: an outdated client is turned away before reaching the
// handler.
func TestAnOutdatedClientIsRejected(t *testing.T) {
	s := newStack(t, func(cfg *grpcserver.Config) {
		cfg.MinClientAPIVersion = "2.0.0"
	})

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	ctx = metadata.AppendToOutgoingContext(ctx, interceptor.MDAPIVersion, "1.0.0")

	_, err := s.grpcClient.HealthPing(ctx, &identityv1.HealthPingRequest{})
	if err == nil {
		t.Fatal("an outdated client was served")
	}

	if got := status.Code(err); got != codes.FailedPrecondition {
		t.Errorf("code = %s, want FAILED_PRECONDITION", got)
	}

	if reason := dizenerrors.ReasonOf(err); reason != dizenerrors.ReasonClientTooOld {
		t.Errorf("reason = %q, want %s", reason, dizenerrors.ReasonClientTooOld)
	}
}

// The whole point of the outbox, exercised against the real database through the same
// repository the service uses.
func TestAnEventWrittenInATransactionIsClaimable(t *testing.T) {
	s := newStack(t)

	err := s.repo.WithTx(t.Context(), func(tx *repository.Repository) error {
		_, err := tx.Outbox().Insert(t.Context(), repository.NewOutboxEvent{
			Aggregate:   "user",
			AggregateID: "u1",
			RoutingKey:  "user.registered",
			Payload:     json.RawMessage(`{"user_id":"u1"}`),
		})

		return err
	})
	if err != nil {
		t.Fatalf("WithTx: %v", err)
	}

	claimed, err := s.repo.Outbox().ClaimPending(t.Context(), 10)
	if err != nil {
		t.Fatalf("ClaimPending: %v", err)
	}

	if len(claimed) != 1 {
		t.Fatalf("%d events claimable, want 1", len(claimed))
	}

	if claimed[0].RoutingKey != "user.registered" {
		t.Errorf("routing key = %q", claimed[0].RoutingKey)
	}
}
