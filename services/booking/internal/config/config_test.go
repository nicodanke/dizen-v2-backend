package config_test

import (
	"strings"
	"testing"
	"time"

	"github.com/nicodanke/dizen-v2-backend/services/booking/internal/config"
)

// setRequired populates the variables the service cannot start without.
func setRequired(t *testing.T) {
	t.Helper()
	t.Setenv("SERVICE_NAME", "booking")
	t.Setenv("DATABASE_URL", "postgres://dizen:dizen@localhost:5432/booking_db?sslmode=disable")
	t.Setenv("REDIS_URL", "redis://localhost:6379/0")
	t.Setenv("AMQP_URL", "amqp://dizen:dizen@localhost:5672/")
}

// The composed config has to load with the defaults of every embedded subsystem, which is
// what keeps the env var names identical across the five services.
func TestLoadComposesEverySubsystem(t *testing.T) {
	setRequired(t)

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.ServiceName != "booking" {
		t.Errorf("ServiceName = %q", cfg.ServiceName)
	}

	if cfg.GRPCPort != 9090 || cfg.HTTPPort != 8080 {
		t.Errorf("ports = %d/%d", cfg.GRPCPort, cfg.HTTPPort)
	}

	if cfg.Database.MaxConns != 10 {
		t.Errorf("Database.MaxConns = %d, want the default of 10", cfg.Database.MaxConns)
	}

	if cfg.AMQP.MaxAttempts != 5 {
		t.Errorf("AMQP.MaxAttempts = %d, want the default of 5", cfg.AMQP.MaxAttempts)
	}

	if cfg.Cache.DefaultTTL != 5*time.Minute {
		t.Errorf("Cache.DefaultTTL = %s", cfg.Cache.DefaultTTL)
	}

	if cfg.Outbox.BatchSize != 100 {
		t.Errorf("Outbox.BatchSize = %d", cfg.Outbox.BatchSize)
	}

	if cfg.JWT.Issuer != "dizen" {
		t.Errorf("JWT.Issuer = %q", cfg.JWT.Issuer)
	}
}

// A missing variable of an embedded subsystem must fail startup naming it, not surface as a
// connection error minutes later.
func TestLoadFailsWhenASubsystemVariableIsMissing(t *testing.T) {
	setRequired(t)
	t.Setenv("DATABASE_URL", "")

	_, err := config.Load()
	if err == nil {
		t.Fatal("Load succeeded without DATABASE_URL")
	}

	if !strings.Contains(err.Error(), "DATABASE_URL") {
		t.Errorf("the error does not name the variable:\n%v", err)
	}
}
