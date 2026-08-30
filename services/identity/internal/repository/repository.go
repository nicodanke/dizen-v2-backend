// Package repository wraps the sqlc-generated Querier and exposes domain types (RF-9).
//
// The rule this package exists to enforce: the service layer depends on the interfaces
// declared here, never on sqlc. Nothing generated crosses this boundary -- no sqlc.Outbox,
// no pgx.Tx in a service signature -- so the domain stays testable with a fake and a
// change to a query cannot ripple into business logic.
package repository

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nicodanke/dizen-v2-backend/pkg/database"
	"github.com/nicodanke/dizen-v2-backend/services/identity/internal/db/sqlc"
)

// Repository is the entry point to persistence for the identity service.
//
// It owns the pool so it can start transactions; the domain repositories hang off it and
// share the same connection.
type Repository struct {
	pool    *pgxpool.Pool
	queries *sqlc.Queries
}

// New builds the repository over a pool.
func New(pool *pgxpool.Pool) *Repository {
	return &Repository{
		pool:    pool,
		queries: sqlc.New(pool),
	}
}

// Outbox returns the outbox repository.
func (r *Repository) Outbox() OutboxRepository {
	return &outboxRepository{queries: r.queries}
}

// WithTx runs fn inside a transaction, handing it a Repository bound to that transaction.
//
// This is the shape that makes the outbox correct (RF-12): the state change and the event
// are written through the same handle, so they either both land or neither does. A caller
// cannot accidentally write the event outside the transaction, because the only handle it
// is given is the transactional one.
func (r *Repository) WithTx(ctx context.Context, fn func(txRepo *Repository) error) error {
	return database.WithTx(ctx, r.pool, func(tx pgx.Tx) error {
		return fn(&Repository{
			pool: r.pool,
			// Bound to the transaction: every query issued through this handle joins it.
			queries: sqlc.New(tx),
		})
	})
}
