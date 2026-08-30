package database

import "testing"

// golang-migrate picks its driver from the URL scheme, and the pgx/v5 driver registers as
// "pgx5". Rewriting it here is what lets DATABASE_URL stay a normal postgres:// URL
// everywhere else: in the pool, in psql and in the compose file.
func TestMigrateDSNRewritesTheScheme(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"postgres://dizen:dizen@db:5432/identity_db?sslmode=disable": "pgx5://dizen:dizen@db:5432/identity_db?sslmode=disable",
		"postgresql://dizen:dizen@db:5432/tours_db":                  "pgx5://dizen:dizen@db:5432/tours_db",
		"pgx5://already:rewritten@db:5432/x":                         "pgx5://already:rewritten@db:5432/x",
		"":                                                           "",
	}

	for input, want := range cases {
		if got := migrateDSN(input); got != want {
			t.Errorf("migrateDSN(%q) = %q, want %q", input, got, want)
		}
	}
}
