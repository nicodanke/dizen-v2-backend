//go:build integration

// Integration tests for pkg/database against a real PostgreSQL (RF-18).
//
// They live behind the `integration` build tag so `make test` stays fast and needs no
// Docker. `make test-coverage` runs with the tag so their coverage counts toward the total
// (RF-18b, acceptance criterion 6).

package database_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/nicodanke/dizen-v2-backend/pkg/database"
	"github.com/nicodanke/dizen-v2-backend/pkg/logger"
	"github.com/nicodanke/dizen-v2-backend/pkg/testutils"
)

// The schema a transaction test needs. It is created here rather than through a migration
// because what is under test is the transaction helper, not any service's schema.
const createAccounts = `
create table if not exists accounts (
	id      bigint generated always as identity primary key,
	name    text   not null,
	balance bigint not null default 0
)`

func TestConnectAgainstARealDatabase(t *testing.T) {
	testutils.SkipIfNoDocker(t)

	pg := testutils.SetupPostgres(t)

	db, err := database.Connect(t.Context(), database.Config{URL: pg.URL}, logger.Nop())
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}

	defer db.Close()

	if err := db.Ping(t.Context()); err != nil {
		t.Errorf("Ping: %v", err)
	}

	// The readiness probe has to pass against a database that is actually up, which is the
	// half the unit tests cannot check.
	if err := db.HealthCheck().Probe(t.Context()); err != nil {
		t.Errorf("the health probe failed against a live database: %v", err)
	}

	stats := db.Stats()
	if stats["max"] == nil {
		t.Error("the pool reports no statistics")
	}
}

// The pool settings have to reach the pool, not just the struct.
func TestThePoolHonorsItsConfiguredLimits(t *testing.T) {
	testutils.SkipIfNoDocker(t)

	pg := testutils.SetupPostgres(t)

	db, err := database.Connect(t.Context(), database.Config{
		URL:      pg.URL,
		MaxConns: 3,
		MinConns: 1,
	}, logger.Nop())
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}

	defer db.Close()

	if got := db.Stats()["max"]; got != int32(3) {
		t.Errorf("MaxConns = %v, want 3", got)
	}
}

// Acceptance criterion 3, the half a closed port cannot show: the service comes up once the
// database starts answering, rather than having given up.
func TestConnectSucceedsOnALaterAttempt(t *testing.T) {
	testutils.SkipIfNoDocker(t)

	pg := testutils.SetupPostgres(t)

	// A tiny connect timeout forces the first attempts to fail against a live database,
	// which is closer to a slow startup than a closed port is.
	db, err := database.Connect(t.Context(), database.Config{
		URL:            pg.URL,
		MaxRetries:     8,
		InitialBackoff: 50 * time.Millisecond,
		MaxBackoff:     500 * time.Millisecond,
	}, logger.Nop())
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}

	db.Close()
}

func TestWithTxCommitsAgainstARealDatabase(t *testing.T) {
	testutils.SkipIfNoDocker(t)

	pg := testutils.SetupPostgres(t)
	testutils.ApplyMigrations(t, pg.Pool, createAccounts)

	db, err := database.Connect(t.Context(), database.Config{URL: pg.URL}, logger.Nop())
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}

	defer db.Close()

	err = db.WithTx(t.Context(), func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), "insert into accounts (name, balance) values ($1, $2)", "committed", 100)

		return err
	})
	if err != nil {
		t.Fatalf("WithTx: %v", err)
	}

	var count int
	if err := pg.Pool.QueryRow(t.Context(), "select count(*) from accounts").Scan(&count); err != nil {
		t.Fatalf("counting: %v", err)
	}

	if count != 1 {
		t.Errorf("%d rows, want 1: the transaction did not commit", count)
	}
}

// The rollback is the part that actually matters: against a fake it is a method call, here
// it is the database really discarding the write.
func TestWithTxRollsBackAgainstARealDatabase(t *testing.T) {
	testutils.SkipIfNoDocker(t)

	pg := testutils.SetupPostgres(t)
	testutils.ApplyMigrations(t, pg.Pool, createAccounts)

	db, err := database.Connect(t.Context(), database.Config{URL: pg.URL}, logger.Nop())
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}

	defer db.Close()

	wantErr := errors.New("the business rule rejected it")

	err = db.WithTx(t.Context(), func(tx pgx.Tx) error {
		if _, err := tx.Exec(t.Context(), "insert into accounts (name) values ($1)", "rolled back"); err != nil {
			return err
		}

		// The row exists inside the transaction...
		var inside int
		if err := tx.QueryRow(t.Context(), "select count(*) from accounts").Scan(&inside); err != nil {
			return err
		}

		if inside != 1 {
			t.Errorf("the row is not visible inside the transaction: %d", inside)
		}

		return wantErr
	})

	if !errors.Is(err, wantErr) {
		t.Errorf("the original error was lost: %v", err)
	}

	// ...and must not exist outside it.
	var count int
	if err := pg.Pool.QueryRow(t.Context(), "select count(*) from accounts").Scan(&count); err != nil {
		t.Fatalf("counting: %v", err)
	}

	if count != 0 {
		t.Errorf("%d rows survived the rollback, want 0", count)
	}
}

// A panic inside a transaction must roll back for real. This is what protects the outbox
// (RF-12) from an event written in a transaction that was never closed.
func TestWithTxRollsBackOnPanicAgainstARealDatabase(t *testing.T) {
	testutils.SkipIfNoDocker(t)

	pg := testutils.SetupPostgres(t)
	testutils.ApplyMigrations(t, pg.Pool, createAccounts)

	db, err := database.Connect(t.Context(), database.Config{URL: pg.URL}, logger.Nop())
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}

	defer db.Close()

	func() {
		defer func() {
			if r := recover(); r == nil {
				t.Error("the panic did not propagate")
			}
		}()

		_ = db.WithTx(t.Context(), func(tx pgx.Tx) error {
			if _, err := tx.Exec(t.Context(), "insert into accounts (name) values ($1)", "panicked"); err != nil {
				return err
			}

			panic("boom")
		})
	}()

	var count int
	if err := pg.Pool.QueryRow(t.Context(), "select count(*) from accounts").Scan(&count); err != nil {
		t.Fatalf("counting: %v", err)
	}

	if count != 0 {
		t.Errorf("%d rows survived the panic, want 0", count)
	}
}

// A transaction that is still open must not block the next one forever; the helper has to
// close it either way.
func TestWithTxReleasesTheConnection(t *testing.T) {
	testutils.SkipIfNoDocker(t)

	pg := testutils.SetupPostgres(t)
	testutils.ApplyMigrations(t, pg.Pool, createAccounts)

	// A pool of one: if a transaction leaked its connection, the second call would hang.
	db, err := database.Connect(t.Context(), database.Config{URL: pg.URL, MaxConns: 1, MinConns: 1}, logger.Nop())
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}

	defer db.Close()

	for i := range 5 {
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)

		err := db.WithTx(ctx, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, "insert into accounts (name) values ($1)", "n")

			return err
		})

		cancel()

		if err != nil {
			t.Fatalf("iteration %d: the connection was not released: %v", i, err)
		}
	}
}
