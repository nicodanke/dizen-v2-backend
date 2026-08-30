package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nicodanke/dizen-v2-backend/pkg/config"
)

// testConfig mirrors the shape a real service declares: the shared base plus its own
// fields.
type testConfig struct {
	config.Base

	DatabaseURL string `env:"DATABASE_URL" validate:"required"`
	FeatureFlag bool   `env:"FEATURE_FLAG" envDefault:"false"`
}

// setEnv sets a variable for the duration of the test and restores it afterwards.
func setEnv(t *testing.T, key, value string) {
	t.Helper()
	t.Setenv(key, value)
}

// setRequired populates every variable a valid testConfig needs.
func setRequired(t *testing.T) {
	t.Helper()
	setEnv(t, "SERVICE_NAME", "identity")
	setEnv(t, "DATABASE_URL", "postgres://localhost:5432/identity_db")
}

func TestLoadAppliesDefaultsWhenOnlyRequiredVarsAreSet(t *testing.T) {
	setRequired(t)

	cfg, err := config.Load[testConfig]()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.ServiceName != "identity" {
		t.Errorf("ServiceName = %q, want identity", cfg.ServiceName)
	}

	if cfg.Environment != config.EnvLocal {
		t.Errorf("Environment = %q, want local", cfg.Environment)
	}

	if cfg.GRPCPort != 9090 || cfg.HTTPPort != 8080 {
		t.Errorf("ports = %d/%d, want 9090/8080", cfg.GRPCPort, cfg.HTTPPort)
	}

	if cfg.ShutdownTimeout != 15*time.Second {
		t.Errorf("ShutdownTimeout = %s, want 15s", cfg.ShutdownTimeout)
	}

	if cfg.GRPCAddress() != ":9090" || cfg.HTTPAddress() != ":8080" {
		t.Errorf("addresses = %q/%q", cfg.GRPCAddress(), cfg.HTTPAddress())
	}
}

// This is the RF-3 acceptance criterion: a missing required variable must stop startup
// with a message that names it.
func TestLoadFailsAndNamesEveryMissingRequiredVariable(t *testing.T) {
	setEnv(t, "SERVICE_NAME", "")
	setEnv(t, "DATABASE_URL", "")

	_, err := config.Load[testConfig]()
	if err == nil {
		t.Fatal("Load succeeded with no required variables set")
	}

	msg := err.Error()

	for _, want := range []string{"SERVICE_NAME", "DATABASE_URL", "is required but was not set"} {
		if !strings.Contains(msg, want) {
			t.Errorf("the error does not mention %q:\n%s", want, msg)
		}
	}
}

func TestLoadRejectsAnUnknownEnvironment(t *testing.T) {
	setRequired(t)
	setEnv(t, "ENVIRONMENT", "prod")

	_, err := config.Load[testConfig]()
	if err == nil {
		t.Fatal("Load accepted ENVIRONMENT=prod")
	}

	if !strings.Contains(err.Error(), "ENVIRONMENT") {
		t.Errorf("the error does not name ENVIRONMENT:\n%v", err)
	}

	// The message has to state what the accepted values are, not just that it is wrong.
	if !strings.Contains(err.Error(), "production") {
		t.Errorf("the error does not list the accepted values:\n%v", err)
	}
}

func TestLoadReadsADotEnvFile(t *testing.T) {
	dir := t.TempDir()
	envFile := filepath.Join(dir, ".env")

	content := "SERVICE_NAME=tours\nDATABASE_URL=postgres://db/tours\nGRPC_PORT=7000\n"
	if err := os.WriteFile(envFile, []byte(content), 0o600); err != nil {
		t.Fatalf("writing the .env file: %v", err)
	}

	cfg, err := config.Load[testConfig](envFile)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.ServiceName != "tours" {
		t.Errorf("ServiceName = %q, want tours", cfg.ServiceName)
	}

	if cfg.GRPCPort != 7000 {
		t.Errorf("GRPCPort = %d, want 7000", cfg.GRPCPort)
	}
}

// Real environment variables must win over the .env file: that is what lets compose and
// Dokploy override a committed file without editing it.
func TestTheEnvironmentWinsOverTheDotEnvFile(t *testing.T) {
	dir := t.TempDir()
	envFile := filepath.Join(dir, ".env")

	content := "SERVICE_NAME=from-file\nDATABASE_URL=postgres://db/x\n"
	if err := os.WriteFile(envFile, []byte(content), 0o600); err != nil {
		t.Fatalf("writing the .env file: %v", err)
	}

	setEnv(t, "SERVICE_NAME", "from-environment")

	cfg, err := config.Load[testConfig](envFile)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.ServiceName != "from-environment" {
		t.Errorf("ServiceName = %q, the .env file overrode the environment", cfg.ServiceName)
	}
}

func TestAMissingDotEnvFileIsNotAnError(t *testing.T) {
	setRequired(t)

	// In production there is no .env file: the configuration comes from the environment.
	cfg, err := config.Load[testConfig](filepath.Join(t.TempDir(), "does-not-exist.env"))
	if err != nil {
		t.Fatalf("Load failed on a missing .env file: %v", err)
	}

	if cfg.ServiceName != "identity" {
		t.Errorf("ServiceName = %q", cfg.ServiceName)
	}
}

func TestEnvironmentPredicates(t *testing.T) {
	t.Parallel()

	cases := []struct {
		env          config.Environment
		isProduction bool
		isLocal      bool
	}{
		{config.EnvProduction, true, false},
		{config.EnvLocal, false, true},
		{config.EnvStaging, false, false},
		{config.EnvTest, false, false},
	}

	for _, tc := range cases {
		if got := tc.env.IsProduction(); got != tc.isProduction {
			t.Errorf("%s.IsProduction() = %v", tc.env, got)
		}

		if got := tc.env.IsLocal(); got != tc.isLocal {
			t.Errorf("%s.IsLocal() = %v", tc.env, got)
		}
	}
}

func TestTracingIsDisabledWithoutAnEndpoint(t *testing.T) {
	t.Parallel()

	if (config.Base{}).TracingEnabled() {
		t.Error("TracingEnabled() is true with an empty OTLP_ENDPOINT")
	}

	if !(config.Base{OTLPEndpoint: "collector:4317"}).TracingEnabled() {
		t.Error("TracingEnabled() is false with an endpoint set")
	}
}

func TestLoadRejectsAnOutOfRangeSampleRatio(t *testing.T) {
	setRequired(t)
	setEnv(t, "TRACE_SAMPLE_RATIO", "1.5")

	_, err := config.Load[testConfig]()
	if err == nil {
		t.Fatal("Load accepted TRACE_SAMPLE_RATIO=1.5")
	}

	if !strings.Contains(err.Error(), "TRACE_SAMPLE_RATIO") {
		t.Errorf("the error does not name TRACE_SAMPLE_RATIO:\n%v", err)
	}
}
