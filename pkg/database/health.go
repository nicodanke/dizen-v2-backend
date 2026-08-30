package database

import (
	"context"

	"github.com/nicodanke/dizen-v2-backend/pkg/health"
)

// CheckName is how the database appears in the /readyz report.
const CheckName = "postgres"

// HealthCheck is the readiness probe for the database.
//
// It is registered by the service, not by this package, so pkg/health never has to know
// what a database is. It is critical: a service that cannot reach its own database has
// nothing useful to answer, and must leave the load balancer rotation while staying alive
// (restarting it would not bring the database back).
func (db *DB) HealthCheck() health.Check {
	return health.Check{
		Name:     CheckName,
		Critical: true,
		Probe: func(ctx context.Context) error {
			return db.Ping(ctx)
		},
	}
}
