// Package service holds the business logic. It depends on repository interfaces, never on
// sqlc, and knows nothing about gRPC or HTTP.
package service

import (
	"context"
	"time"

	"github.com/nicodanke/dizen-v2-backend/pkg/version"
)

// BuildInfo is what a health ping reports.
type BuildInfo struct {
	Service    string
	Version    string
	Commit     string
	ServerTime time.Time
}

// HealthService answers the reference RPC of RF-19.
//
// It carries no domain logic on purpose: its job is to prove the template works end to
// end. Real authentication arrives with PRD-01.
type HealthService struct {
	serviceName string

	// now is injectable so a test can assert on the reported time.
	now func() time.Time
}

// NewHealthService builds the service.
func NewHealthService(serviceName string) *HealthService {
	return &HealthService{serviceName: serviceName, now: time.Now}
}

// Ping returns the identity of the running build.
func (s *HealthService) Ping(context.Context) (BuildInfo, error) {
	info := version.Get()

	return BuildInfo{
		Service:    s.serviceName,
		Version:    info.Version,
		Commit:     info.Commit,
		ServerTime: s.now().UTC(),
	}, nil
}
