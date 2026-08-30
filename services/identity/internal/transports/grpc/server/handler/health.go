// Package handler translates between the generated protobuf types and the service layer.
//
// The translation exists so the service layer never sees a protobuf type: a change to the
// contract stops at this boundary instead of reaching into the business logic.
package handler

import (
	"context"

	"google.golang.org/protobuf/types/known/timestamppb"

	identityv1 "github.com/nicodanke/dizen-v2-backend/pkg/genproto/dizen/identity/v1"
	"github.com/nicodanke/dizen-v2-backend/services/identity/internal/service"
)

// HealthHandler serves dizen.identity.v1.HealthService.
type HealthHandler struct {
	identityv1.UnimplementedHealthServiceServer

	svc *service.HealthService
}

// NewHealthHandler builds the handler.
func NewHealthHandler(svc *service.HealthService) *HealthHandler {
	return &HealthHandler{svc: svc}
}

// HealthPing implements the RPC.
func (h *HealthHandler) HealthPing(
	ctx context.Context,
	_ *identityv1.HealthPingRequest,
) (*identityv1.HealthPingResponse, error) {
	info, err := h.svc.Ping(ctx)
	if err != nil {
		// Returned as-is: the errors interceptor decides what the client is told.
		return nil, err
	}

	return &identityv1.HealthPingResponse{
		Service:    info.Service,
		Version:    info.Version,
		Commit:     info.Commit,
		ServerTime: timestamppb.New(info.ServerTime),
	}, nil
}
