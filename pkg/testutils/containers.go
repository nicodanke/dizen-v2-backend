// Package testutils brings up real dependencies for the integration tests (RF-18).
//
// Real containers rather than fakes, because what these tests are for is exactly the part a
// fake cannot check: that the SQL is valid, that the migrations apply, that a Redis SCAN
// walks the keyspace the way the code assumes, that a message really lands in the
// dead-letter queue. A fake would confirm our own assumptions back to us.
//
// Every helper registers its own cleanup with t.Cleanup, so a test never leaks a container
// even when it fails (acceptance criterion 6).
package testutils

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/modules/rabbitmq"
	tcredis "github.com/testcontainers/testcontainers-go/modules/redis"
	"github.com/testcontainers/testcontainers-go/wait"
)

// Images are pinned to the same versions the compose file runs, so a test cannot pass
// against a Postgres the production environment does not use.
const (
	// PostgresImage matches deploy/docker-compose.yml.
	PostgresImage = "postgres:18.6-alpine"

	// PostGISImage is the multi-arch community build; the official postgis/postgis
	// publishes linux/amd64 only and the team develops on Apple Silicon.
	PostGISImage = "imresamu/postgis:18-3.6-alpine"

	// RedisImage matches the compose file.
	RedisImage = "redis:8-alpine"

	// RabbitMQImage matches the compose file.
	RabbitMQImage = "rabbitmq:4-management-alpine"
)

// startupTimeout bounds bringing a container up. It is generous because the first run pulls
// the image, and a CI runner with a cold cache should not fail for that.
const startupTimeout = 3 * time.Minute

// Postgres is a running database and everything needed to reach it.
type Postgres struct {
	// URL is the connection string.
	URL string

	// Pool is an open pool, closed automatically when the test ends.
	Pool *pgxpool.Pool

	container *postgres.PostgresContainer
}

// SetupPostgres starts a PostgreSQL container and returns an open pool.
//
// The database is per test, not shared: a shared one makes tests order-dependent, and an
// order-dependent suite is one that fails on CI and passes locally.
func SetupPostgres(t *testing.T, opts ...PostgresOption) *Postgres {
	t.Helper()

	settings := postgresSettings{image: PostgresImage, database: "dizen_test"}
	for _, opt := range opts {
		opt(&settings)
	}

	ctx := context.Background()

	container, err := postgres.Run(ctx, settings.image,
		postgres.WithDatabase(settings.database),
		postgres.WithUsername("dizen"),
		postgres.WithPassword("dizen"),
		postgres.BasicWaitStrategies(),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(startupTimeout),
		),
	)
	if err != nil {
		t.Fatalf("starting PostgreSQL: %v", err)
	}

	registerTerminate(t, container)

	url, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("reading the connection string: %v", err)
	}

	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatalf("opening the pool: %v", err)
	}

	t.Cleanup(pool.Close)

	if err := pool.Ping(ctx); err != nil {
		t.Fatalf("pinging the database: %v", err)
	}

	return &Postgres{URL: url, Pool: pool, container: container}
}

// postgresSettings are the knobs SetupPostgres exposes.
type postgresSettings struct {
	image    string
	database string
}

// PostgresOption customizes the container.
type PostgresOption func(*postgresSettings)

// WithPostGIS uses the PostGIS image, which only tours_db needs (01 section 5).
func WithPostGIS() PostgresOption {
	return func(s *postgresSettings) { s.image = PostGISImage }
}

// WithDatabase sets the database name.
func WithDatabase(name string) PostgresOption {
	return func(s *postgresSettings) { s.database = name }
}

// Snapshot marks the current state so Restore can return to it.
//
// It is what lets one container serve a whole test file: taking a snapshot after the
// migrations and restoring between tests costs milliseconds, where a container per test
// costs seconds.
func (p *Postgres) Snapshot(t *testing.T) {
	t.Helper()

	if err := p.container.Snapshot(context.Background()); err != nil {
		t.Fatalf("taking the snapshot: %v", err)
	}
}

// Restore returns the database to the last snapshot.
func (p *Postgres) Restore(t *testing.T) {
	t.Helper()

	// The pool is closed first: restoring drops the database, and an open connection to it
	// makes the drop fail.
	p.Pool.Reset()

	if err := p.container.Restore(context.Background()); err != nil {
		t.Fatalf("restoring the snapshot: %v", err)
	}
}

// Redis is a running cache.
type Redis struct {
	// URL is the connection string.
	URL string

	// Client is an open client, closed automatically when the test ends.
	Client *redis.Client
}

// SetupRedis starts a Redis container and returns an open client.
func SetupRedis(t *testing.T) *Redis {
	t.Helper()

	ctx := context.Background()

	container, err := tcredis.Run(ctx, RedisImage,
		testcontainers.WithWaitStrategy(
			wait.ForLog("Ready to accept connections").WithStartupTimeout(startupTimeout),
		),
	)
	if err != nil {
		t.Fatalf("starting Redis: %v", err)
	}

	registerTerminate(t, container)

	url, err := container.ConnectionString(ctx)
	if err != nil {
		t.Fatalf("reading the connection string: %v", err)
	}

	opts, err := redis.ParseURL(url)
	if err != nil {
		t.Fatalf("parsing the Redis URL %q: %v", url, err)
	}

	client := redis.NewClient(opts)

	t.Cleanup(func() { _ = client.Close() })

	if err := client.Ping(ctx).Err(); err != nil {
		t.Fatalf("pinging Redis: %v", err)
	}

	return &Redis{URL: url, Client: client}
}

// AMQP is a running broker.
type AMQP struct {
	// URL is the connection string.
	URL string
}

// SetupAMQP starts a RabbitMQ container scoped to one test.
func SetupAMQP(t *testing.T) *AMQP {
	t.Helper()

	url, terminate, err := StartAMQP(context.Background())
	if err != nil {
		t.Fatalf("starting RabbitMQ: %v", err)
	}

	t.Cleanup(terminate)

	return &AMQP{URL: url}
}

// StartAMQP starts a RabbitMQ container without a *testing.T, for use from TestMain.
//
// RabbitMQ is by far the slowest of the three to boot, so a package that needs it shares one
// broker across its tests. Sharing requires TestMain: registering the teardown with
// t.Cleanup on the first test would tear the container down as soon as that test finished,
// leaving every later test dialing a closed port.
//
// Tests that share a broker isolate themselves with a unique queue name instead.
func StartAMQP(ctx context.Context) (url string, terminate func(), err error) {
	container, err := rabbitmq.Run(ctx, RabbitMQImage,
		rabbitmq.WithAdminUsername("dizen"),
		rabbitmq.WithAdminPassword("dizen"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("Server startup complete").WithStartupTimeout(startupTimeout),
		),
	)
	if err != nil {
		return "", nil, fmt.Errorf("starting RabbitMQ: %w", err)
	}

	// The teardown deliberately starts from a fresh context rather than from the one
	// passed in: by the time it runs, the caller's context is usually already done, and a
	// container left behind is worse than a slow teardown.
	terminate = func() {
		stopCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()

		_ = container.Terminate(stopCtx)
	}

	url, err = container.AmqpURL(ctx)
	if err != nil {
		terminate()

		return "", nil, fmt.Errorf("reading the AMQP URL: %w", err)
	}

	return url, terminate, nil
}

// DockerAvailable reports whether the Docker daemon is reachable, for use from TestMain
// where there is no *testing.T to skip with.
func DockerAvailable() bool {
	provider, err := testcontainers.NewDockerProvider()
	if err != nil {
		return false
	}

	defer func() { _ = provider.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err = provider.DaemonHost(ctx)

	return err == nil
}

// registerTerminate makes sure the container is removed when the test ends, however it ends
// (acceptance criterion 6: the integration test cleans up after itself).
func registerTerminate(t *testing.T, container testcontainers.Container) {
	t.Helper()

	t.Cleanup(func() {
		// A generous timeout of its own: the test context may already be canceled, and a
		// container left behind is worse than a slow teardown.
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		if err := container.Terminate(ctx); err != nil {
			t.Logf("could not remove the container: %v", err)
		}
	})
}

// SkipIfNoDocker skips the test when Docker is not reachable, with a message that says why.
//
// It exists so `go test ./...` on a machine without Docker reports skipped rather than a
// wall of failures nobody can act on.
func SkipIfNoDocker(t *testing.T) {
	t.Helper()

	provider, err := testcontainers.NewDockerProvider()
	if err != nil {
		t.Skipf("Docker is not available: %v", err)
	}

	defer func() { _ = provider.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if _, err := provider.DaemonHost(ctx); err != nil {
		t.Skipf("the Docker daemon is not reachable: %v", err)
	}
}

// ApplyMigrations runs the SQL of a migrations filesystem against the pool.
//
// It exists so a repository test starts from the same schema production has, rather than
// from a hand-written CREATE TABLE that can drift from the real migration.
func ApplyMigrations(t *testing.T, pool *pgxpool.Pool, statements ...string) {
	t.Helper()

	ctx := context.Background()

	for i, statement := range statements {
		if _, err := pool.Exec(ctx, statement); err != nil {
			t.Fatalf("applying statement %d: %v\n%s", i+1, err, statement)
		}
	}
}

// TruncateTables empties the given tables, for a test that wants a clean slate without
// paying for a snapshot restore.
func TruncateTables(t *testing.T, pool *pgxpool.Pool, tables ...string) {
	t.Helper()

	for _, table := range tables {
		if _, err := pool.Exec(context.Background(), fmt.Sprintf("truncate table %s restart identity cascade", table)); err != nil {
			t.Fatalf("truncating %s: %v", table, err)
		}
	}
}
