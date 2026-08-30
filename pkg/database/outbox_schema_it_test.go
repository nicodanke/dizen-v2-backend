//go:build integration

package database_test

import (
	"os"
	"path/filepath"
	"testing"
)

// realOutboxMigration is the path to the base migration every service ships.
//
// It is read at run time rather than embedded because go:embed cannot reach outside the
// package directory. Reading the real file is the point: a copy in testdata would drift
// from what production applies, and this test exists precisely to catch that.
const realOutboxMigration = "../../services/identity/internal/db/migrations/000001_base.up.sql"

// outboxSchema returns the SQL of the real base migration.
func outboxSchema(t *testing.T) string {
	t.Helper()

	path, err := filepath.Abs(realOutboxMigration)
	if err != nil {
		t.Fatalf("resolving %s: %v", realOutboxMigration, err)
	}

	sql, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the base migration: %v", err)
	}

	return string(sql)
}
