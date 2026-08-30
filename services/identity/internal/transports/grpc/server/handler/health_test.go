package handler_test

import (
	"testing"

	identityv1 "github.com/nicodanke/dizen-v2-backend/pkg/genproto/dizen/identity/v1"
	"github.com/nicodanke/dizen-v2-backend/services/identity/internal/service"
	"github.com/nicodanke/dizen-v2-backend/services/identity/internal/transports/grpc/server/handler"
)

// The handler's only job is translation, so that is what is tested: the service layer never
// sees a protobuf type, and the protobuf response carries what the service returned.
func TestHealthPingTranslatesTheServiceResult(t *testing.T) {
	t.Parallel()

	h := handler.NewHealthHandler(service.NewHealthService("identity"))

	resp, err := h.HealthPing(t.Context(), &identityv1.HealthPingRequest{})
	if err != nil {
		t.Fatalf("HealthPing: %v", err)
	}

	if resp.GetService() != "identity" {
		t.Errorf("service = %q", resp.GetService())
	}

	if resp.GetVersion() == "" {
		t.Error("the version was not carried over")
	}

	if resp.GetServerTime() == nil {
		t.Error("the server time was not carried over")
	}
}
