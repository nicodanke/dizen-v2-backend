package database

import (
	"embed"
	"errors"
	"fmt"
	"strings"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/pgx/v5"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/rs/zerolog"
)

// ErrMigration wraps every migration failure so a caller can branch on it.
var ErrMigration = errors.New("migration failed")

// Migrate applies every pending migration.
//
// Migrations are embedded in the binary and applied at container startup (RF-7), never by
// hand in production: a schema that depends on somebody remembering to run a command is a
// schema that eventually drifts between environments.
//
// golang-migrate takes an advisory lock, so several replicas starting at once is safe: one
// applies the migrations and the rest wait and then find nothing to do.
func Migrate(dsn string, migrations embed.FS, path string, log zerolog.Logger) error {
	migrator, err := newMigrator(dsn, migrations, path)
	if err != nil {
		return err
	}

	defer closeMigrator(migrator, log)

	before, dirty, err := migrator.Version()
	if err != nil && !errors.Is(err, migrate.ErrNilVersion) {
		return fmt.Errorf("%w: reading the current version: %w", ErrMigration, err)
	}

	// A dirty state means a previous migration died half applied. Continuing would apply
	// the next one on top of an unknown schema, so it stops here and asks for a human.
	if dirty {
		return fmt.Errorf("%w: the schema is marked dirty at version %d and needs manual repair",
			ErrMigration, before)
	}

	if err := migrator.Up(); err != nil {
		if errors.Is(err, migrate.ErrNoChange) {
			log.Info().Uint("version", before).Msg("the schema is already up to date")

			return nil
		}

		return fmt.Errorf("%w: applying migrations: %w", ErrMigration, err)
	}

	after, _, err := migrator.Version()
	if err != nil {
		return fmt.Errorf("%w: reading the resulting version: %w", ErrMigration, err)
	}

	log.Info().
		Uint("from", before).
		Uint("to", after).
		Msg("migrations applied")

	return nil
}

// MigrateDown rolls back the last n migrations. It exists for development; production only
// ever migrates forward.
func MigrateDown(dsn string, migrations embed.FS, path string, steps int, log zerolog.Logger) error {
	migrator, err := newMigrator(dsn, migrations, path)
	if err != nil {
		return err
	}

	defer closeMigrator(migrator, log)

	if err := migrator.Steps(-steps); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("%w: rolling back %d steps: %w", ErrMigration, steps, err)
	}

	return nil
}

// newMigrator builds a migrator over the embedded filesystem.
func newMigrator(dsn string, migrations embed.FS, path string) (*migrate.Migrate, error) {
	source, err := iofs.New(migrations, path)
	if err != nil {
		return nil, fmt.Errorf("%w: reading the embedded migrations at %q: %w", ErrMigration, path, err)
	}

	migrator, err := migrate.NewWithSourceInstance("iofs", source, migrateDSN(dsn))
	if err != nil {
		return nil, fmt.Errorf("%w: building the migrator: %w", ErrMigration, err)
	}

	return migrator, nil
}

// migrateScheme is the name the pgx/v5 driver registers itself under in golang-migrate.
const migrateScheme = "pgx5"

// migrateDSN rewrites the connection string scheme for the migrator.
//
// golang-migrate selects its driver from the URL scheme, and the pgx/v5 driver registers as
// "pgx5" rather than "postgres". Rewriting it here means DATABASE_URL stays a normal
// postgres:// URL everywhere else -- in the pool, in psql, in the compose file -- and the
// one place that needs a different name does the translation itself.
func migrateDSN(dsn string) string {
	for _, scheme := range []string{"postgres://", "postgresql://"} {
		if after, found := strings.CutPrefix(dsn, scheme); found {
			return migrateScheme + "://" + after
		}
	}

	return dsn
}

// closeMigrator releases both halves, logging rather than failing: by the time it runs the
// migrations either applied or did not, and that outcome has already been decided.
func closeMigrator(migrator *migrate.Migrate, log zerolog.Logger) {
	sourceErr, dbErr := migrator.Close()

	if sourceErr != nil {
		log.Warn().Err(sourceErr).Msg("closing the migration source")
	}

	if dbErr != nil {
		log.Warn().Err(dbErr).Msg("closing the migrator connection")
	}
}

// ensure the pgx driver is linked in; it registers itself with golang-migrate on import.
var _ = pgx.Postgres{}
