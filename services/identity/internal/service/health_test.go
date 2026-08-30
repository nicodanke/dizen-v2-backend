package service_test

import (
	"testing"
	"time"

	"github.com/nicodanke/dizen-v2-backend/services/identity/internal/service"
)

func TestPingReportsTheBuild(t *testing.T) {
	t.Parallel()

	info, err := service.NewHealthService("identity").Ping(t.Context())
	if err != nil {
		t.Fatalf("Ping: %v", err)
	}

	if info.Service != "identity" {
		t.Errorf("Service = %q", info.Service)
	}

	if info.Version == "" {
		t.Error("Version is empty")
	}

	if info.Commit == "" {
		t.Error("Commit is empty")
	}

	// UTC, not local: a timestamp whose zone depends on where the container runs is one
	// nobody can compare across services.
	if _, offset := info.ServerTime.Zone(); offset != 0 {
		t.Errorf("ServerTime is not UTC: %s", info.ServerTime)
	}

	if delta := time.Since(info.ServerTime); delta > time.Minute || delta < -time.Minute {
		t.Errorf("ServerTime is off by %s", delta)
	}
}
