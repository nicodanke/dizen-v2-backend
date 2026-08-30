package database_test

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/nicodanke/dizen-v2-backend/pkg/database"
	"github.com/nicodanke/dizen-v2-backend/pkg/health"
	"github.com/nicodanke/dizen-v2-backend/pkg/logger"
)

// deadAddress returns a host:port where nothing listens, by binding a port and releasing
// it. It is how the tests simulate a database that is down without needing Docker.
func deadAddress(t *testing.T) (host string, port string) {
	t.Helper()

	var lc net.ListenConfig

	listener, err := lc.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("binding a port: %v", err)
	}

	addr := listener.Addr().String()

	if err := listener.Close(); err != nil {
		t.Fatalf("releasing the port: %v", err)
	}

	host, port, err = net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("splitting %q: %v", addr, err)
	}

	return host, port
}

// dsnToNowhere builds a valid connection string pointing at a closed port.
func dsnToNowhere(t *testing.T) string {
	t.Helper()

	host, port := deadAddress(t)

	return "postgres://dizen:dizen@" + net.JoinHostPort(host, port) + "/identity_db?sslmode=disable"
}

// This is acceptance criterion 3 of PRD-00: with Postgres down the service retries with
// backoff instead of failing on the first attempt.
func TestConnectRetriesWithBackoffWhenTheDatabaseIsDown(t *testing.T) {
	t.Parallel()

	cfg := database.Config{
		URL:            dsnToNowhere(t),
		MaxRetries:     4,
		InitialBackoff: 20 * time.Millisecond,
		MaxBackoff:     100 * time.Millisecond,
		ConnectTimeout: 500 * time.Millisecond,
	}

	start := time.Now()

	_, err := database.Connect(t.Context(), cfg, logger.Nop())

	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("Connect succeeded against a closed port")
	}

	if !errors.Is(err, database.ErrUnreachable) {
		t.Errorf("the error is not ErrUnreachable: %v", err)
	}

	// The error has to say how many attempts were made, so an operator reading the crash
	// log knows the retry actually happened.
	if !strings.Contains(err.Error(), "4 attempts") {
		t.Errorf("the error does not report the number of attempts: %v", err)
	}

	// Backoff of 20 + 40 + 80 = 140ms between the four attempts. Anything clearly below
	// that means it retried without waiting.
	if elapsed < 140*time.Millisecond {
		t.Errorf("it took %s: the backoff was not applied between attempts", elapsed)
	}
}

// The backoff must be capped: without a ceiling, ten attempts would take hours.
func TestTheBackoffIsCapped(t *testing.T) {
	t.Parallel()

	cfg := database.Config{
		URL:            dsnToNowhere(t),
		MaxRetries:     6,
		InitialBackoff: 10 * time.Millisecond,
		MaxBackoff:     20 * time.Millisecond,
		ConnectTimeout: 500 * time.Millisecond,
	}

	start := time.Now()

	_, _ = database.Connect(t.Context(), cfg, logger.Nop())

	elapsed := time.Since(start)

	// Uncapped it would be 10+20+40+80+160 = 310ms; capped at 20ms it is 10+20*4 = 90ms.
	if elapsed > 2*time.Second {
		t.Errorf("it took %s: the backoff cap was not applied", elapsed)
	}
}

// A service being shut down while it waits for its database must stop immediately rather
// than sit out the remaining backoff.
func TestConnectHonorsCancellationWhileWaiting(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())

	cfg := database.Config{
		URL:            dsnToNowhere(t),
		MaxRetries:     50,
		InitialBackoff: 200 * time.Millisecond,
		MaxBackoff:     10 * time.Second,
		ConnectTimeout: 200 * time.Millisecond,
	}

	go func() {
		time.Sleep(300 * time.Millisecond)
		cancel()
	}()

	start := time.Now()

	_, err := database.Connect(ctx, cfg, logger.Nop())

	if err == nil {
		t.Fatal("Connect succeeded")
	}

	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Errorf("it took %s: cancellation was ignored", elapsed)
	}

	if !errors.Is(err, context.Canceled) {
		t.Errorf("the error does not report the cancellation: %v", err)
	}
}

func TestConnectRejectsAnUnparseableURL(t *testing.T) {
	t.Parallel()

	_, err := database.Connect(t.Context(), database.Config{URL: "this is not a dsn"}, logger.Nop())
	if err == nil {
		t.Fatal("an invalid DATABASE_URL was accepted")
	}

	if !errors.Is(err, database.ErrInvalidConfig) {
		t.Errorf("the error is not ErrInvalidConfig: %v", err)
	}

	// A bad connection string must fail immediately, not after ten retries: retrying a
	// typo never fixes it.
	if errors.Is(err, database.ErrUnreachable) {
		t.Error("it retried a connection string that could never work")
	}
}

func TestConnectRejectsAnEmptyURL(t *testing.T) {
	t.Parallel()

	_, err := database.Connect(t.Context(), database.Config{}, logger.Nop())
	if !errors.Is(err, database.ErrInvalidConfig) {
		t.Errorf("the error is not ErrInvalidConfig: %v", err)
	}
}

func TestValidateRejectsMinAboveMax(t *testing.T) {
	t.Parallel()

	cfg := database.Config{URL: "postgres://x/y", MinConns: 20, MaxConns: 5}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("MinConns above MaxConns was accepted")
	}

	if !strings.Contains(err.Error(), "DATABASE_MIN_CONNS") {
		t.Errorf("the error does not name the variable: %v", err)
	}
}

// The other half of criterion 3: the service must not report ready while the database is
// down. The probe is what enforces it, and it has to be critical.
func TestTheHealthCheckIsCriticalSoTheServiceIsNotReadyWithoutTheDatabase(t *testing.T) {
	t.Parallel()

	// A probe standing in for a database that is down, wired the way DB.HealthCheck wires
	// the real one.
	check := health.Check{
		Name:     database.CheckName,
		Critical: true,
		Probe: func(context.Context) error {
			return errors.New("dial tcp: connect: connection refused")
		},
	}

	registry := health.NewRegistry("identity", "v0.1.0")
	registry.Register(check)

	report := registry.Check(t.Context())

	if report.Ready() {
		t.Error("the service reported ready with its database down")
	}

	if report.Checks[0].Name != "postgres" {
		t.Errorf("the check name is %q, want postgres", report.Checks[0].Name)
	}
}
