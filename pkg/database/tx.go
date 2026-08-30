package database

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// Tx is the subset of a pgx transaction the repositories use. Declaring it here rather than
// taking pgx.Tx directly is what lets a repository be exercised with a fake.
type Tx interface {
	pgx.Tx
}

// Beginner starts transactions. Both *pgxpool.Pool and pgx.Tx satisfy it, which is what
// makes nested WithTx calls work.
type Beginner interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

// WithTx runs fn inside a transaction, committing on success and rolling back on error or
// panic (RF-9).
//
// The signature is the point: a caller cannot forget the rollback, and cannot commit a
// transaction whose function returned an error. The panic case matters because of the
// outbox (RF-12): an event written in a transaction that was never closed would block the
// rows behind it.
func WithTx(ctx context.Context, beginner Beginner, fn func(tx pgx.Tx) error) (err error) {
	tx, err := beginner.Begin(ctx)
	if err != nil {
		return fmt.Errorf("beginning the transaction: %w", err)
	}

	defer func() {
		if r := recover(); r != nil {
			// The rollback error is deliberately discarded: the panic is the real
			// failure and must keep unwinding.
			_ = tx.Rollback(ctx)

			panic(r)
		}

		if err == nil {
			return
		}

		// Rolling back an already finished transaction is not a failure worth reporting:
		// it happens when the context was canceled and pgx closed it first.
		if rollbackErr := tx.Rollback(ctx); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
			err = fmt.Errorf("%w (rollback also failed: %w)", err, rollbackErr)
		}
	}()

	if err = fn(tx); err != nil {
		return err
	}

	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("committing the transaction: %w", err)
	}

	return nil
}

// WithTx runs fn inside a transaction on this pool.
func (db *DB) WithTx(ctx context.Context, fn func(tx pgx.Tx) error) error {
	return WithTx(ctx, db.pool, fn)
}
