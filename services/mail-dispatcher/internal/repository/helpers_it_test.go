//go:build integration

package repository_test

import (
	"errors"

	"github.com/jackc/pgx/v5"
)

// pgxTx aliases the transaction type so the concurrency test reads without importing pgx
// into every signature.
type pgxTx = pgx.Tx

// errAborted stands in for a business failure that aborts a transaction.
var errAborted = errors.New("the business rule aborted the transaction")
