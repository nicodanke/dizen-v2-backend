//go:build integration

package database_test

import (
	"embed"
	"errors"
	"testing"

	"github.com/nicodanke/dizen-v2-backend/pkg/database"
	"github.com/nicodanke/dizen-v2-backend/pkg/logger"
	"github.com/nicodanke/dizen-v2-backend/pkg/testutils"
)

// testdata holds a two-step migration used to check that Migrate applies, is idempotent and
// rolls back. It is embedded the same way a service embeds its own.
//
//go:embed testdata/migrations/*.sql
var testMigrations embed.FS

const testMigrationsPath = "testdata/migrations"

// Acceptance criterion 6: the migrations apply against a real Postgres, and the container is
// cleaned up when the test ends.
func TestMigrateAppliesEveryMigration(t *testing.T) {
	testutils.SkipIfNoDocker(t)

	pg := testutils.SetupPostgres(t)

	if err := database.Migrate(pg.URL, testMigrations, testMigrationsPath, logger.Nop()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	// Both tables exist, so both steps ran.
	for _, table := range []string{"widgets", "gadgets"} {
		var exists bool

		err := pg.Pool.QueryRow(t.Context(),
			"select exists (select 1 from information_schema.tables where table_name = $1)", table,
		).Scan(&exists)
		if err != nil {
			t.Fatalf("checking %s: %v", table, err)
		}

		if !exists {
			t.Errorf("the table %s was not created", table)
		}
	}

	// The column added by the second migration is there, which is what proves the order
	// was respected rather than just that both files ran.
	var hasColumn bool

	err := pg.Pool.QueryRow(t.Context(),
		"select exists (select 1 from information_schema.columns where table_name = 'widgets' and column_name = 'label')",
	).Scan(&hasColumn)
	if err != nil {
		t.Fatalf("checking the column: %v", err)
	}

	if !hasColumn {
		t.Error("the second migration did not apply on top of the first")
	}
}

// Running twice must be a no-op. Every replica applies migrations at startup (RF-7), so this
// is the normal case, not an edge one.
func TestMigrateIsIdempotent(t *testing.T) {
	testutils.SkipIfNoDocker(t)

	pg := testutils.SetupPostgres(t)

	if err := database.Migrate(pg.URL, testMigrations, testMigrationsPath, logger.Nop()); err != nil {
		t.Fatalf("first Migrate: %v", err)
	}

	if err := database.Migrate(pg.URL, testMigrations, testMigrationsPath, logger.Nop()); err != nil {
		t.Fatalf("second Migrate: %v", err)
	}

	var version int
	if err := pg.Pool.QueryRow(t.Context(), "select version from schema_migrations").Scan(&version); err != nil {
		t.Fatalf("reading the version: %v", err)
	}

	if version != 2 {
		t.Errorf("version = %d, want 2", version)
	}
}

func TestMigrateDownRollsBack(t *testing.T) {
	testutils.SkipIfNoDocker(t)

	pg := testutils.SetupPostgres(t)

	if err := database.Migrate(pg.URL, testMigrations, testMigrationsPath, logger.Nop()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	if err := database.MigrateDown(pg.URL, testMigrations, testMigrationsPath, 1, logger.Nop()); err != nil {
		t.Fatalf("MigrateDown: %v", err)
	}

	var exists bool

	err := pg.Pool.QueryRow(t.Context(),
		"select exists (select 1 from information_schema.tables where table_name = 'gadgets')",
	).Scan(&exists)
	if err != nil {
		t.Fatalf("checking gadgets: %v", err)
	}

	if exists {
		t.Error("the rollback did not drop the table the second migration created")
	}
}

// A schema left dirty by a half-applied migration must stop the service rather than let the
// next migration run on top of an unknown state.
func TestMigrateRefusesADirtySchema(t *testing.T) {
	testutils.SkipIfNoDocker(t)

	pg := testutils.SetupPostgres(t)

	if err := database.Migrate(pg.URL, testMigrations, testMigrationsPath, logger.Nop()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	if _, err := pg.Pool.Exec(t.Context(), "update schema_migrations set dirty = true"); err != nil {
		t.Fatalf("marking it dirty: %v", err)
	}

	err := database.Migrate(pg.URL, testMigrations, testMigrationsPath, logger.Nop())
	if err == nil {
		t.Fatal("Migrate ran against a dirty schema")
	}

	if !errors.Is(err, database.ErrMigration) {
		t.Errorf("the error is not ErrMigration: %v", err)
	}
}

// The service's real migrations have to apply, not just the ones written for this test.
// This is what would catch a syntax error in the base migration before a deploy does.
func TestTheOutboxMigrationApplies(t *testing.T) {
	testutils.SkipIfNoDocker(t)

	pg := testutils.SetupPostgres(t)

	testutils.ApplyMigrations(t, pg.Pool, outboxSchema(t))

	// The partial index the worker's query depends on has to exist: without it, claiming
	// pending events degrades to a sequential scan of the whole table.
	var hasIndex bool

	err := pg.Pool.QueryRow(t.Context(),
		"select exists (select 1 from pg_indexes where indexname = 'outbox_pending_idx')",
	).Scan(&hasIndex)
	if err != nil {
		t.Fatalf("checking the index: %v", err)
	}

	if !hasIndex {
		t.Error("outbox_pending_idx does not exist")
	}
}
