package health_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/nicodanke/dizen-v2-backend/pkg/health"
)

// up is a probe that always succeeds.
func up(context.Context) error { return nil }

// down builds a probe that always fails with the given message.
func down(msg string) health.CheckFunc {
	return func(context.Context) error { return errors.New(msg) }
}

func TestLivenessAlwaysAnswersOK(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/livez", nil)

	health.LivenessHandler("identity", "v1.0.0")(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}

	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("the body is not JSON: %v", err)
	}

	if body["service"] != "identity" {
		t.Errorf("service = %q", body["service"])
	}
}

func TestReadinessIsOKWithEveryCheckUp(t *testing.T) {
	t.Parallel()

	registry := health.NewRegistry("identity", "v1.0.0")
	registry.Register(health.Check{Name: "postgres", Probe: up, Critical: true})
	registry.Register(health.Check{Name: "redis", Probe: up, Critical: true})
	registry.Register(health.Check{Name: "amqp", Probe: up, Critical: true})

	rec := httptest.NewRecorder()
	registry.ReadinessHandler()(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/readyz", nil))

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200. Body: %s", rec.Code, rec.Body.String())
	}
}

// This is the RF-6 acceptance criterion: 503 plus the detail of what is failing.
func TestReadinessReturns503AndSaysWhichDependencyIsDown(t *testing.T) {
	t.Parallel()

	registry := health.NewRegistry("tours", "v1.0.0")
	registry.Register(health.Check{Name: "postgres", Probe: up, Critical: true})
	registry.Register(health.Check{Name: "redis", Probe: down("connection refused"), Critical: true})
	registry.Register(health.Check{Name: "amqp", Probe: up, Critical: true})

	rec := httptest.NewRecorder()
	registry.ReadinessHandler()(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/readyz", nil))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}

	var report health.Report
	if err := json.Unmarshal(rec.Body.Bytes(), &report); err != nil {
		t.Fatalf("the body is not JSON: %v", err)
	}

	if report.Status != health.StatusDown {
		t.Errorf("status = %q, want down", report.Status)
	}

	byName := map[string]health.Result{}
	for _, result := range report.Checks {
		byName[result.Name] = result
	}

	if len(byName) != 3 {
		t.Fatalf("3 checks expected, got %d", len(byName))
	}

	redis := byName["redis"]
	if redis.Status != health.StatusDown {
		t.Errorf("redis status = %q", redis.Status)
	}

	// The point of the criterion: the response has to say *what* is failing.
	if redis.Error != "connection refused" {
		t.Errorf("the redis error is not reported: %q", redis.Error)
	}

	if byName["postgres"].Status != health.StatusUp {
		t.Error("postgres was reported as down")
	}
}

// A non-critical dependency that fails is reported but must not pull the service out of
// rotation.
func TestANonCriticalFailureKeepsTheServiceReady(t *testing.T) {
	t.Parallel()

	registry := health.NewRegistry("tours", "v1.0.0")
	registry.Register(health.Check{Name: "postgres", Probe: up, Critical: true})
	registry.Register(health.Check{Name: "search-index", Probe: down("timeout"), Critical: false})

	rec := httptest.NewRecorder()
	registry.ReadinessHandler()(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/readyz", nil))

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200: a non-critical failure must not unready the service", rec.Code)
	}

	var report health.Report
	_ = json.Unmarshal(rec.Body.Bytes(), &report)

	var found bool

	for _, result := range report.Checks {
		if result.Name == "search-index" && result.Status == health.StatusDown {
			found = true
		}
	}

	if !found {
		t.Error("the non-critical failure was not reported")
	}
}

// A probe that hangs must not hang the endpoint: it has to be bounded and reported down.
func TestAHangingProbeIsBoundedByTheTimeout(t *testing.T) {
	t.Parallel()

	registry := health.NewRegistry("identity", "v1.0.0")
	registry.SetTimeout(50 * time.Millisecond)
	registry.Register(health.Check{
		Name:     "slow",
		Critical: true,
		Probe: func(ctx context.Context) error {
			select {
			case <-time.After(10 * time.Second):
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		},
	})

	start := time.Now()

	rec := httptest.NewRecorder()
	registry.ReadinessHandler()(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/readyz", nil))

	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("the endpoint took %s: the probe timeout was not applied", elapsed)
	}

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", rec.Code)
	}
}

// Probes must run concurrently: readiness latency should be the slowest dependency, not
// the sum.
func TestProbesRunConcurrently(t *testing.T) {
	t.Parallel()

	const probeDelay = 100 * time.Millisecond

	registry := health.NewRegistry("identity", "v1.0.0")

	for _, name := range []string{"a", "b", "c", "d"} {
		registry.Register(health.Check{
			Name:     name,
			Critical: true,
			Probe: func(context.Context) error {
				time.Sleep(probeDelay)

				return nil
			},
		})
	}

	start := time.Now()
	report := registry.Check(context.Background())
	elapsed := time.Since(start)

	if !report.Ready() {
		t.Error("the report came back not ready")
	}

	// Serially this would take 400ms; concurrently it is around 100ms.
	if elapsed > 3*probeDelay {
		t.Errorf("the probes took %s: they ran serially", elapsed)
	}
}

func TestAnEmptyRegistryIsReady(t *testing.T) {
	t.Parallel()

	// A service with no dependencies -- as mail-dispatcher is before RF-11 -- must answer
	// ready, not fail for lack of checks.
	report := health.NewRegistry("mail-dispatcher", "v1.0.0").Check(context.Background())

	if !report.Ready() {
		t.Error("an empty registry reported not ready")
	}
}
