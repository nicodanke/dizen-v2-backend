// Package database opens and supervises the PostgreSQL connection pool, applies the
// migrations and exposes the transaction helper the repositories use (RF-7, RF-9).
//
// One database per service, no cross-access (hard rule 2). This package knows nothing
// about any schema: it hands out a pool and a transaction, and the repositories built on
// top own the SQL.
package database

import (
	"fmt"
	"time"
)

// Pool defaults. They are deliberately modest: five services against five databases on one
// VPS, where an oversized pool wastes memory on both ends and hides slow queries instead of
// surfacing them.
const (
	// DefaultMaxConns is the ceiling on open connections.
	DefaultMaxConns = 10
	// DefaultMinConns is how many are kept warm, so a cold burst does not pay the
	// handshake.
	DefaultMinConns = 2
	// DefaultMaxConnLifetime recycles a connection periodically, which is what keeps a
	// deploy of the database from leaving stale connections behind.
	DefaultMaxConnLifetime = 30 * time.Minute
	// DefaultMaxConnIdleTime releases connections nobody is using.
	DefaultMaxConnIdleTime = 5 * time.Minute
	// DefaultHealthCheckPeriod is how often the pool probes idle connections.
	DefaultHealthCheckPeriod = 1 * time.Minute
	// DefaultConnectTimeout bounds a single connection attempt.
	DefaultConnectTimeout = 5 * time.Second
)

// Startup retry defaults. Acceptance criterion 3 of PRD-00: a service starting with
// Postgres down must retry with backoff and must not become ready until the database
// answers.
const (
	// DefaultMaxRetries is how many attempts are made before giving up.
	DefaultMaxRetries = 10
	// DefaultInitialBackoff is the wait before the second attempt.
	DefaultInitialBackoff = 500 * time.Millisecond
	// DefaultMaxBackoff caps the exponential growth.
	DefaultMaxBackoff = 30 * time.Second
)

// Config describes the connection. It is embedded by each service's config struct, so the
// env var names below are the ones that appear in every .env.example.
type Config struct {
	// URL is the full connection string. Required.
	URL string `env:"DATABASE_URL" validate:"required"`

	// MaxConns is the ceiling on open connections.
	MaxConns int32 `env:"DATABASE_MAX_CONNS" envDefault:"10" validate:"min=1"`

	// MinConns is how many connections are kept warm.
	MinConns int32 `env:"DATABASE_MIN_CONNS" envDefault:"2" validate:"min=0"`

	// MaxConnLifetime is how long a connection may live before being recycled.
	MaxConnLifetime time.Duration `env:"DATABASE_MAX_CONN_LIFETIME" envDefault:"30m"`

	// MaxConnIdleTime is how long an idle connection is kept.
	MaxConnIdleTime time.Duration `env:"DATABASE_MAX_CONN_IDLE_TIME" envDefault:"5m"`

	// HealthCheckPeriod is how often the pool probes idle connections.
	HealthCheckPeriod time.Duration `env:"DATABASE_HEALTH_CHECK_PERIOD" envDefault:"1m"`

	// ConnectTimeout bounds one connection attempt.
	ConnectTimeout time.Duration `env:"DATABASE_CONNECT_TIMEOUT" envDefault:"5s"`

	// MaxRetries is how many startup attempts are made before giving up.
	MaxRetries int `env:"DATABASE_MAX_RETRIES" envDefault:"10" validate:"min=1"`

	// InitialBackoff is the wait before the second startup attempt.
	InitialBackoff time.Duration `env:"DATABASE_INITIAL_BACKOFF" envDefault:"500ms"`

	// MaxBackoff caps the exponential growth of the startup backoff.
	MaxBackoff time.Duration `env:"DATABASE_MAX_BACKOFF" envDefault:"30s"`

	// RunMigrations applies pending migrations at startup. RF-7 requires migrations to be
	// applied when the container starts, never by hand in production.
	RunMigrations bool `env:"DATABASE_RUN_MIGRATIONS" envDefault:"false"`
}

// withDefaults fills the zero values, so a Config built in code rather than from the
// environment is still usable.
func (c Config) withDefaults() Config {
	if c.MaxConns <= 0 {
		c.MaxConns = DefaultMaxConns
	}

	if c.MinConns < 0 {
		c.MinConns = DefaultMinConns
	}

	if c.MaxConnLifetime <= 0 {
		c.MaxConnLifetime = DefaultMaxConnLifetime
	}

	if c.MaxConnIdleTime <= 0 {
		c.MaxConnIdleTime = DefaultMaxConnIdleTime
	}

	if c.HealthCheckPeriod <= 0 {
		c.HealthCheckPeriod = DefaultHealthCheckPeriod
	}

	if c.ConnectTimeout <= 0 {
		c.ConnectTimeout = DefaultConnectTimeout
	}

	if c.MaxRetries <= 0 {
		c.MaxRetries = DefaultMaxRetries
	}

	if c.InitialBackoff <= 0 {
		c.InitialBackoff = DefaultInitialBackoff
	}

	if c.MaxBackoff <= 0 {
		c.MaxBackoff = DefaultMaxBackoff
	}

	return c
}

// Validate reports a configuration that cannot work, before any connection is attempted.
func (c Config) Validate() error {
	if c.URL == "" {
		return fmt.Errorf("%w: DATABASE_URL is empty", ErrInvalidConfig)
	}

	if c.MinConns > c.MaxConns {
		return fmt.Errorf("%w: DATABASE_MIN_CONNS (%d) is above DATABASE_MAX_CONNS (%d)",
			ErrInvalidConfig, c.MinConns, c.MaxConns)
	}

	return nil
}
