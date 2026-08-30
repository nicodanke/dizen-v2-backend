package grpcserver_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/nicodanke/dizen-v2-backend/pkg/config"
	dizenerrors "github.com/nicodanke/dizen-v2-backend/pkg/errors"
	commonv1 "github.com/nicodanke/dizen-v2-backend/pkg/genproto/dizen/common/v1"
	identityv1 "github.com/nicodanke/dizen-v2-backend/pkg/genproto/dizen/identity/v1"
	"github.com/nicodanke/dizen-v2-backend/pkg/grpcserver"
	"github.com/nicodanke/dizen-v2-backend/pkg/grpcserver/interceptor"
	"github.com/nicodanke/dizen-v2-backend/pkg/logger"
	"github.com/nicodanke/dizen-v2-backend/pkg/observability/metrics"
)

// stubHealth is a HealthService whose behavior each test decides.
type stubHealth struct {
	identityv1.UnimplementedHealthServiceServer

	respond func() (*identityv1.HealthPingResponse, error)
}

func (s *stubHealth) HealthPing(
	_ context.Context,
	_ *identityv1.HealthPingRequest,
) (*identityv1.HealthPingResponse, error) {
	return s.respond()
}

// harness is a server running on a real port with a client already connected.
type harness struct {
	client identityv1.HealthServiceClient
	logs   *bytes.Buffer
}

// newHarness starts a server on an ephemeral port and returns a connected client. Cleanup
// is registered with t.Cleanup so no test has to remember to tear it down.
func newHarness(t *testing.T, cfg grpcserver.Config, deps grpcserver.Dependencies, svc *stubHealth) *harness {
	t.Helper()

	logs := &bytes.Buffer{}

	cfg.Address = "127.0.0.1:0"
	cfg.Logger = logger.New(logger.Options{ServiceName: "identity", Level: "debug", Output: logs})

	server, err := grpcserver.New(cfg, deps)
	if err != nil {
		t.Fatalf("grpcserver.New: %v", err)
	}

	identityv1.RegisterHealthServiceServer(server.Registrar(), svc)

	if err := server.Listen(t.Context()); err != nil {
		t.Fatalf("Listen: %v", err)
	}

	go func() {
		_ = server.Serve(context.Background())
	}()

	conn, err := grpc.NewClient(server.Address(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("dialing: %v", err)
	}

	t.Cleanup(func() {
		_ = conn.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		_ = server.Shutdown(ctx)
	})

	return &harness{client: identityv1.NewHealthServiceClient(conn), logs: logs}
}

// okService answers successfully.
func okService() *stubHealth {
	return &stubHealth{respond: func() (*identityv1.HealthPingResponse, error) {
		return &identityv1.HealthPingResponse{Service: "identity"}, nil
	}}
}

func TestTheServerAnswersARegisteredRPC(t *testing.T) {
	t.Parallel()

	h := newHarness(t, grpcserver.Config{Environment: config.EnvTest}, grpcserver.Dependencies{}, okService())

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := h.client.HealthPing(ctx, &identityv1.HealthPingRequest{})
	if err != nil {
		t.Fatalf("HealthPing: %v", err)
	}

	if resp.GetService() != "identity" {
		t.Errorf("service = %q", resp.GetService())
	}
}

// Acceptance criterion 5 of PRD-00: a panic in a handler reaches the client as INTERNAL
// with no detail, and the full stack ends up in the log.
func TestAPanicBecomesInternalWithoutLeakingAndIsFullyLogged(t *testing.T) {
	t.Parallel()

	const secret = "connection string postgres://user:hunter2@db/identity"

	svc := &stubHealth{respond: func() (*identityv1.HealthPingResponse, error) {
		panic(secret)
	}}

	h := newHarness(t, grpcserver.Config{Environment: config.EnvTest}, grpcserver.Dependencies{}, svc)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := h.client.HealthPing(ctx, &identityv1.HealthPingRequest{})
	if err == nil {
		t.Fatal("the panic did not surface as an error")
	}

	st, ok := status.FromError(err)
	if !ok {
		t.Fatalf("the error is not a gRPC status: %v", err)
	}

	if st.Code() != codes.Internal {
		t.Errorf("code = %s, want INTERNAL", st.Code())
	}

	// The client must learn nothing: neither the panic value nor the stack.
	if strings.Contains(st.Message(), secret) || strings.Contains(st.Message(), "hunter2") {
		t.Errorf("the response leaked the panic value: %q", st.Message())
	}

	if strings.Contains(st.Message(), "goroutine") {
		t.Errorf("the response leaked the stack: %q", st.Message())
	}

	// The server, on the other hand, must have recorded everything.
	logs := h.logs.String()

	if !strings.Contains(logs, secret) {
		t.Errorf("the panic value is missing from the log:\n%s", logs)
	}

	if !strings.Contains(logs, "goroutine") {
		t.Errorf("the stack is missing from the log:\n%s", logs)
	}

	if !strings.Contains(logs, `"level":"error"`) {
		t.Errorf("the panic was not logged at error level:\n%s", logs)
	}
}

// The server has to keep serving after a panic: recovering is pointless if the process
// dies on the next call.
func TestTheServerSurvivesAPanic(t *testing.T) {
	t.Parallel()

	shouldPanic := true

	svc := &stubHealth{respond: func() (*identityv1.HealthPingResponse, error) {
		if shouldPanic {
			shouldPanic = false

			panic("boom")
		}

		return &identityv1.HealthPingResponse{Service: "identity"}, nil
	}}

	h := newHarness(t, grpcserver.Config{Environment: config.EnvTest}, grpcserver.Dependencies{}, svc)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, _ = h.client.HealthPing(ctx, &identityv1.HealthPingRequest{})

	resp, err := h.client.HealthPing(ctx, &identityv1.HealthPingRequest{})
	if err != nil {
		t.Fatalf("the second call failed after a panic: %v", err)
	}

	if resp.GetService() != "identity" {
		t.Errorf("service = %q", resp.GetService())
	}
}

func TestTheAccessLogRecordsMethodCodeAndLatency(t *testing.T) {
	t.Parallel()

	h := newHarness(t, grpcserver.Config{Environment: config.EnvTest}, grpcserver.Dependencies{}, okService())

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if _, err := h.client.HealthPing(ctx, &identityv1.HealthPingRequest{}); err != nil {
		t.Fatalf("HealthPing: %v", err)
	}

	var found bool

	for line := range strings.SplitSeq(strings.TrimSpace(h.logs.String()), "\n") {
		var entry map[string]any
		if json.Unmarshal([]byte(line), &entry) != nil {
			continue
		}

		if entry["message"] != "gRPC call" {
			continue
		}

		found = true

		if entry[logger.FieldMethod] != identityv1.HealthService_HealthPing_FullMethodName {
			t.Errorf("method = %v", entry[logger.FieldMethod])
		}

		if entry["code"] != "OK" {
			t.Errorf("code = %v", entry["code"])
		}

		if entry["latency"] == nil {
			t.Error("the latency is missing")
		}
	}

	if !found {
		t.Errorf("no access line was emitted:\n%s", h.logs.String())
	}
}

func TestMetricsCountTheCall(t *testing.T) {
	t.Parallel()

	registry := metrics.NewRegistry("identity")

	h := newHarness(t,
		grpcserver.Config{Environment: config.EnvTest, Metrics: registry},
		grpcserver.Dependencies{},
		okService(),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if _, err := h.client.HealthPing(ctx, &identityv1.HealthPingRequest{}); err != nil {
		t.Fatalf("HealthPing: %v", err)
	}

	families, err := registry.Prometheus().Gather()
	if err != nil {
		t.Fatalf("Gather: %v", err)
	}

	var counted bool

	for _, family := range families {
		if family.GetName() != "dizen_grpc_requests_total" {
			continue
		}

		for _, metric := range family.GetMetric() {
			if metric.GetCounter().GetValue() > 0 {
				counted = true
			}
		}
	}

	if !counted {
		t.Error("dizen_grpc_requests_total did not record the call")
	}
}

// RF-2c and acceptance criterion 2c: a client declaring a contract version older than the
// minimum gets CLIENT_TOO_OLD.
func TestAClientBelowTheMinimumVersionGetsClientTooOld(t *testing.T) {
	t.Parallel()

	h := newHarness(t,
		grpcserver.Config{Environment: config.EnvTest, MinClientAPIVersion: "1.4.0"},
		grpcserver.Dependencies{},
		okService(),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	ctx = metadata.AppendToOutgoingContext(ctx, interceptor.MDAPIVersion, "1.2.0")

	_, err := h.client.HealthPing(ctx, &identityv1.HealthPingRequest{})
	if err == nil {
		t.Fatal("an outdated client was accepted")
	}

	if got := status.Code(err); got != codes.FailedPrecondition {
		t.Errorf("code = %s, want FAILED_PRECONDITION", got)
	}

	// The client decides on the reason, never on the message text (03 section 1).
	if reason := dizenerrors.ReasonOf(err); reason != interceptor.ReasonClientTooOld {
		t.Errorf("reason = %q, want %s", reason, interceptor.ReasonClientTooOld)
	}
}

func TestACurrentClientIsAccepted(t *testing.T) {
	t.Parallel()

	h := newHarness(t,
		grpcserver.Config{Environment: config.EnvTest, MinClientAPIVersion: "1.4.0"},
		grpcserver.Dependencies{},
		okService(),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	ctx = metadata.AppendToOutgoingContext(ctx, interceptor.MDAPIVersion, "1.5.2")

	if _, err := h.client.HealthPing(ctx, &identityv1.HealthPingRequest{}); err != nil {
		t.Fatalf("a current client was rejected: %v", err)
	}
}

// An already published app does not send the header; rejecting it would break exactly the
// clients the check is meant to protect (decision D-12).
func TestAClientWithoutTheVersionHeaderIsLetThrough(t *testing.T) {
	t.Parallel()

	h := newHarness(t,
		grpcserver.Config{Environment: config.EnvTest, MinClientAPIVersion: "1.4.0"},
		grpcserver.Dependencies{},
		okService(),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if _, err := h.client.HealthPing(ctx, &identityv1.HealthPingRequest{}); err != nil {
		t.Fatalf("a client without x-api-version was rejected: %v", err)
	}
}

func TestAnInvalidMinimumVersionFailsAtConstruction(t *testing.T) {
	t.Parallel()

	_, err := grpcserver.New(
		grpcserver.Config{Address: "127.0.0.1:0", MinClientAPIVersion: "not-semver"},
		grpcserver.Dependencies{},
	)
	if err == nil {
		t.Fatal("an unparseable MIN_CLIENT_API_VERSION was accepted")
	}

	if !strings.Contains(err.Error(), "MIN_CLIENT_API_VERSION") {
		t.Errorf("the error does not name the variable: %v", err)
	}
}

// The validate interceptor enforces the constraints declared in the protos.
func TestValidationRejectsAMalformedRequest(t *testing.T) {
	t.Parallel()

	// GeoPoint bounds latitude to [-90, 90] in the proto.
	point := &commonv1.GeoPoint{Lat: 120, Lng: 0}

	validator, err := interceptor.NewValidator()
	if err != nil {
		t.Fatalf("NewValidator: %v", err)
	}

	if err := validator.Validate(point); err == nil {
		t.Error("a latitude of 120 degrees was accepted")
	}

	if err := validator.Validate(&commonv1.GeoPoint{Lat: -34.6, Lng: -58.4}); err != nil {
		t.Errorf("a valid point was rejected: %v", err)
	}
}

func TestShutdownIsGraceful(t *testing.T) {
	t.Parallel()

	server, err := grpcserver.New(
		grpcserver.Config{Address: "127.0.0.1:0", Environment: config.EnvTest, Logger: logger.Nop()},
		grpcserver.Dependencies{},
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if err := server.Listen(t.Context()); err != nil {
		t.Fatalf("Listen: %v", err)
	}

	served := make(chan error, 1)
	go func() { served <- server.Serve(context.Background()) }()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	select {
	case err := <-served:
		if err != nil {
			t.Errorf("Serve returned an error on a clean shutdown: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Error("Serve did not return after Shutdown")
	}
}

// The Dependencies seam is where the three interceptors that arrive with a later RF plug
// in -- rate limit (RF-10), auth (RF-14) and authz (PRD-14). This test proves an injected
// interceptor really runs inside the chain and can reject a call, so those RFs only have
// to supply the function and never touch the assembly order.
func TestAnInjectedDependencyRunsInTheChain(t *testing.T) {
	t.Parallel()

	var reached bool

	rejecting := func(
		_ context.Context,
		_ any,
		_ *grpc.UnaryServerInfo,
		_ grpc.UnaryHandler,
	) (any, error) {
		reached = true

		return nil, status.Error(codes.PermissionDenied, "denied by the injected interceptor")
	}

	svc := &stubHealth{respond: func() (*identityv1.HealthPingResponse, error) {
		t.Error("the handler ran even though the interceptor rejected the call")

		return nil, nil
	}}

	h := newHarness(t,
		grpcserver.Config{Environment: config.EnvTest},
		grpcserver.Dependencies{Authenticator: rejecting},
		svc,
	)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := h.client.HealthPing(ctx, &identityv1.HealthPingRequest{})
	if err == nil {
		t.Fatal("the injected interceptor did not reject the call")
	}

	if !reached {
		t.Error("the injected interceptor never ran")
	}

	if got := status.Code(err); got != codes.PermissionDenied {
		t.Errorf("code = %s, want PERMISSION_DENIED", got)
	}
}

// A nil dependency has to be skipped, not replaced by a permissive stub: a chain that
// silently authorizes everything is worse than one that visibly lacks the link.
func TestANilDependencyIsSkipped(t *testing.T) {
	t.Parallel()

	h := newHarness(t,
		grpcserver.Config{Environment: config.EnvTest},
		grpcserver.Dependencies{RateLimiter: nil, Authenticator: nil, Authorizer: nil},
		okService(),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if _, err := h.client.HealthPing(ctx, &identityv1.HealthPingRequest{}); err != nil {
		t.Fatalf("a nil dependency broke the chain: %v", err)
	}
}

// RF-13 through the real chain: a handler returning a plain error tells the client nothing
// and the log everything. The unit tests of pkg/errors cover the mapping; this one proves
// the interceptor is actually wired into the chain.
func TestAPlainErrorFromAHandlerLeaksNothingOverTheWire(t *testing.T) {
	t.Parallel()

	const secret = `pq: relation "users" does not exist at postgres://dizen:hunter2@db:5432`

	svc := &stubHealth{respond: func() (*identityv1.HealthPingResponse, error) {
		return nil, errors.New(secret)
	}}

	h := newHarness(t, grpcserver.Config{Environment: config.EnvTest}, grpcserver.Dependencies{}, svc)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := h.client.HealthPing(ctx, &identityv1.HealthPingRequest{})
	if err == nil {
		t.Fatal("the error did not surface")
	}

	st, _ := status.FromError(err)

	if st.Code() != codes.Internal {
		t.Errorf("code = %s, want INTERNAL", st.Code())
	}

	for _, leaked := range []string{"hunter2", "postgres://", "relation"} {
		if strings.Contains(st.Message(), leaked) {
			t.Errorf("the response leaked %q: %q", leaked, st.Message())
		}
	}

	if reason := dizenerrors.ReasonOf(err); reason != dizenerrors.ReasonInternal {
		t.Errorf("reason = %q, want %s", reason, dizenerrors.ReasonInternal)
	}

	if !strings.Contains(h.logs.String(), "hunter2") {
		t.Errorf("the cause is missing from the log:\n%s", h.logs.String())
	}
}

// A domain error, by contrast, reaches the client with its declared code, reason and
// message: that is what the app branches on.
func TestADomainErrorReachesTheClientIntact(t *testing.T) {
	t.Parallel()

	svc := &stubHealth{respond: func() (*identityv1.HealthPingResponse, error) {
		return nil, dizenerrors.
			PermissionDenied(dizenerrors.ReasonTourNotEntitled, "the tour requires a subscription").
			WithCause(errors.New("entitlement row missing for user u1")).
			WithMetadata("tour_id", "t1")
	}}

	h := newHarness(t, grpcserver.Config{Environment: config.EnvTest}, grpcserver.Dependencies{}, svc)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := h.client.HealthPing(ctx, &identityv1.HealthPingRequest{})

	st, _ := status.FromError(err)

	if st.Code() != codes.PermissionDenied {
		t.Errorf("code = %s, want PERMISSION_DENIED", st.Code())
	}

	if st.Message() != "the tour requires a subscription" {
		t.Errorf("message = %q", st.Message())
	}

	if reason := dizenerrors.ReasonOf(err); reason != dizenerrors.ReasonTourNotEntitled {
		t.Errorf("reason = %q", reason)
	}

	// The cause stays internal even for a declared error.
	if strings.Contains(st.Message(), "entitlement row missing") {
		t.Errorf("the response leaked the cause: %q", st.Message())
	}
}
