package database

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"

	"github.com/nicodanke/dizen-v2-backend/pkg/logger"
)

// Sentinel errors. They are static so callers can branch on them with errors.Is instead of
// matching message text.
var (
	// ErrInvalidConfig means the configuration cannot work as given.
	ErrInvalidConfig = errors.New("invalid database configuration")
	// ErrUnreachable means every connection attempt failed.
	ErrUnreachable = errors.New("the database did not answer")
)

// DB is the connection pool plus the configuration it was built with.
type DB struct {
	pool *pgxpool.Pool
	cfg  Config
}

// Connect opens the pool, retrying with exponential backoff until the database answers or
// the attempts run out.
//
// This is acceptance criterion 3 of PRD-00. The retry exists because compose and Dokploy
// start the service and its database at the same time: failing on the first attempt would
// turn a two-second race into a crash loop. What must NOT happen is the service reporting
// itself ready while the database is still down, which is why readiness is a separate
// check (see HealthCheck) and not a side effect of this call.
func Connect(ctx context.Context, cfg Config, log zerolog.Logger) (*DB, error) {
	cfg = cfg.withDefaults()

	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	poolCfg, err := poolConfig(cfg)
	if err != nil {
		return nil, err
	}

	var lastErr error

	backoff := cfg.InitialBackoff

	for attempt := 1; attempt <= cfg.MaxRetries; attempt++ {
		pool, err := tryConnect(ctx, poolCfg, cfg.ConnectTimeout)
		if err == nil {
			log.Info().
				Int("attempt", attempt).
				Int32("max_conns", cfg.MaxConns).
				Msg("connected to PostgreSQL")

			return &DB{pool: pool, cfg: cfg}, nil
		}

		lastErr = err

		log.Warn().
			Err(err).
			Int("attempt", attempt).
			Int("max_attempts", cfg.MaxRetries).
			Dur("retry_in", backoff).
			Msg("PostgreSQL did not answer, retrying")

		// The last failure is not followed by a wait: it would delay the error by one
		// backoff period for no reason.
		if attempt == cfg.MaxRetries {
			break
		}

		if err := sleep(ctx, backoff); err != nil {
			return nil, err
		}

		backoff = nextBackoff(backoff, cfg.MaxBackoff)
	}

	return nil, fmt.Errorf("%w after %d attempts: %w", ErrUnreachable, cfg.MaxRetries, lastErr)
}

// tryConnect makes one attempt, bounded by its own timeout.
func tryConnect(ctx context.Context, poolCfg *pgxpool.Config, timeout time.Duration) (*pgxpool.Pool, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("building the pool: %w", err)
	}

	// NewWithConfig is lazy: without this ping a database that is down would look like a
	// successful connection until the first query.
	if err := pool.Ping(ctx); err != nil {
		pool.Close()

		return nil, fmt.Errorf("pinging the database: %w", err)
	}

	return pool, nil
}

// poolConfig translates Config into a pgxpool configuration.
func poolConfig(cfg Config) (*pgxpool.Config, error) {
	poolCfg, err := pgxpool.ParseConfig(cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("%w: DATABASE_URL could not be parsed: %w", ErrInvalidConfig, err)
	}

	poolCfg.MaxConns = cfg.MaxConns
	poolCfg.MinConns = cfg.MinConns
	poolCfg.MaxConnLifetime = cfg.MaxConnLifetime
	poolCfg.MaxConnIdleTime = cfg.MaxConnIdleTime
	poolCfg.HealthCheckPeriod = cfg.HealthCheckPeriod
	poolCfg.ConnConfig.ConnectTimeout = cfg.ConnectTimeout

	return poolCfg, nil
}

// nextBackoff doubles the wait, capped at the maximum.
func nextBackoff(current, maximum time.Duration) time.Duration {
	next := current * 2
	if next > maximum {
		return maximum
	}

	return next
}

// sleep waits, honoring cancellation. A service being shut down while it waits for its
// database must stop immediately rather than sit out the backoff.
func sleep(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Pool exposes the underlying pool, which is what the generated sqlc code takes.
func (db *DB) Pool() *pgxpool.Pool {
	return db.pool
}

// Ping checks the database answers. It is what backs the readiness probe.
func (db *DB) Ping(ctx context.Context) error {
	if err := db.pool.Ping(ctx); err != nil {
		return fmt.Errorf("pinging the database: %w", err)
	}

	return nil
}

// Close releases the pool. It is safe to call more than once.
func (db *DB) Close() {
	if db.pool != nil {
		db.pool.Close()
	}
}

// Stats exposes pool statistics for the operational log and for debugging exhaustion.
func (db *DB) Stats() map[string]any {
	stats := db.pool.Stat()

	return map[string]any{
		"acquired":         stats.AcquiredConns(),
		"idle":             stats.IdleConns(),
		"total":            stats.TotalConns(),
		"max":              stats.MaxConns(),
		"acquire_count":    stats.AcquireCount(),
		"empty_acquires":   stats.EmptyAcquireCount(),
		"canceled_acquire": stats.CanceledAcquireCount(),
	}
}

// LogStats writes the pool statistics. Called on shutdown, where a pool that was permanently
// exhausted is worth knowing about.
func (db *DB) LogStats(ctx context.Context) {
	logger.Ctx(ctx).Debug().Fields(db.Stats()).Msg("PostgreSQL pool statistics")
}
