// Entry point of the admin service.
//
// main.go is pure composition, in this order: read config -> open dependencies -> build
// services -> start transports -> wait for a signal -> shut down gracefully. There is no
// business logic here and no package-level state: everything is injected explicitly.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/rs/zerolog"

	"github.com/nicodanke/dizen-v2-backend/pkg/amqp"
	"github.com/nicodanke/dizen-v2-backend/pkg/bootstrap"
	"github.com/nicodanke/dizen-v2-backend/pkg/cache"
	"github.com/nicodanke/dizen-v2-backend/pkg/database"
	"github.com/nicodanke/dizen-v2-backend/pkg/grpcserver"
	"github.com/nicodanke/dizen-v2-backend/pkg/grpcserver/interceptor"
	"github.com/nicodanke/dizen-v2-backend/pkg/health"
	"github.com/nicodanke/dizen-v2-backend/pkg/httpserver"
	"github.com/nicodanke/dizen-v2-backend/pkg/jwt"
	"github.com/nicodanke/dizen-v2-backend/pkg/logger"
	"github.com/nicodanke/dizen-v2-backend/pkg/observability/metrics"
	"github.com/nicodanke/dizen-v2-backend/pkg/observability/sentry"
	"github.com/nicodanke/dizen-v2-backend/pkg/observability/tracing"
	"github.com/nicodanke/dizen-v2-backend/pkg/outbox"
	"github.com/nicodanke/dizen-v2-backend/pkg/version"
	"github.com/nicodanke/dizen-v2-backend/services/admin/internal/config"
	"github.com/nicodanke/dizen-v2-backend/services/admin/internal/db/migrations"
	"github.com/nicodanke/dizen-v2-backend/services/admin/internal/repository"
)

const serviceName = "admin"

func main() {
	migrateOnly := flag.Bool("migrate", false, "apply pending migrations and exit")
	envFile := flag.String("env", ".env", "path to the .env file")
	healthcheck := flag.Bool("healthcheck", false, "probe the running service and exit")
	flag.Parse()

	// The production image is distroless: no curl, no shell. The HEALTHCHECK is therefore
	// the binary probing itself, before any configuration is loaded.
	if *healthcheck {
		if err := runHealthcheck(*envFile); err != nil {
			fmt.Fprintf(os.Stderr, "%s: unhealthy: %v\n", serviceName, err)
			os.Exit(1)
		}

		return
	}

	if err := run(context.Background(), *envFile, *migrateOnly); err != nil {
		// The logger may not exist yet when configuration fails, so the last resort is
		// stderr. Anything past that point is logged structured.
		fmt.Fprintf(os.Stderr, "%s: fatal: %v\n", serviceName, err)
		os.Exit(1)
	}
}

// runHealthcheck probes the local /livez, reading only the port from the configuration.
func runHealthcheck(envFile string) error {
	cfg, err := config.Load(envFile)
	if err != nil {
		return err
	}

	return bootstrap.Healthcheck(cfg.HTTPPort)
}

// run wires everything and blocks until the service stops.
func run(ctx context.Context, envFile string, migrateOnly bool) error {
	cfg, err := config.Load(envFile)
	if err != nil {
		return err
	}

	build := version.Get()

	// Before the logger, because the reporter is one of its writers: every log at error
	// level or above becomes a Sentry event (PRD-24 RF-2). An empty DSN disables it.
	reporter, err := sentry.Setup(sentry.Options{
		DSN:         cfg.SentryDSN,
		Environment: cfg.Environment.String(),
		Release:     sentry.Release(cfg.ServiceName, build.Version, build.Commit),
		ServiceName: cfg.ServiceName,
	})
	if err != nil {
		return err
	}

	// Flushed on the way out so the error that caused the shutdown is not the one that
	// never arrives.
	defer func() { _ = reporter.Close() }()

	log := logger.New(logger.Options{
		ServiceName: cfg.ServiceName,
		Level:       cfg.LogLevel,
		Extra:       []io.Writer{reporter.Writer()},
		// Colored console on a developer machine, JSON everywhere else so Loki can
		// parse it.
		Pretty: cfg.Environment.IsLocal(),
	})

	log.Info().
		Str("environment", cfg.Environment.String()).
		Str("version", build.Version).
		Str("commit", build.Commit).
		Msg("starting")

	// Migrations run before anything else opens a pool, so no query ever meets a schema
	// that is halfway migrated.
	if cfg.Database.RunMigrations || migrateOnly {
		if err := database.Migrate(cfg.Database.URL, migrations.FS, migrations.Path, log); err != nil {
			return err
		}
	}

	if migrateOnly {
		return nil
	}

	return serve(ctx, cfg, log)
}

// serve opens the dependencies, builds the transports and runs them.
func serve(ctx context.Context, cfg *config.Config, log zerolog.Logger) error {
	shutdownTracing, err := tracing.Setup(ctx, tracing.Options{
		ServiceName:    cfg.ServiceName,
		ServiceVersion: version.Get().Version,
		Environment:    cfg.Environment.String(),
		Endpoint:       cfg.OTLPEndpoint,
		SampleRatio:    cfg.TraceSampleRatio,
		Insecure:       !cfg.Environment.IsProduction(),
	})
	if err != nil {
		return err
	}

	db, err := database.Connect(ctx, cfg.Database, log)
	if err != nil {
		return err
	}

	redis, err := cache.NewRedis(ctx, cfg.Cache, cfg.ServiceName)
	if err != nil {
		return err
	}

	broker, err := amqp.Connect(cfg.AMQP, log)
	if err != nil {
		return err
	}

	publisher, err := amqp.NewPublisher(broker)
	if err != nil {
		return err
	}

	// Readiness reports every dependency, and the criticality of each decides whether a
	// failure takes the service out of rotation (RF-6).
	healthRegistry := health.NewRegistry(cfg.ServiceName, version.Get().Version)
	healthRegistry.Register(db.HealthCheck())
	healthRegistry.Register(redis.HealthCheck())
	healthRegistry.Register(broker.HealthCheck())

	metricsRegistry := metrics.NewRegistry(cfg.ServiceName)

	// The key set holds public keys only: this service validates tokens, it cannot mint
	// one.
	if _, err := jwt.LoadKeySet(cfg.JWT); err != nil {
		return err
	}

	repo := repository.New(db.Pool())

	outboxWorker := outbox.NewWorker(repo.OutboxStore(), publisher, cfg.Outbox, log)

	// contextcheck traces into the stream recovery interceptor, which reads its context
	// from the stream at call time. Building the server performs no I/O and has no context
	// to propagate: giving it one would be a signature that lies.
	//nolint:contextcheck // the interceptors take their context from each call
	grpcSrv, err := buildGRPC(cfg, log, metricsRegistry)
	if err != nil {
		return err
	}

	// The domain RPCs of this service arrive with its own PRD. Until then the gRPC
	// server serves only the standard health service, which the probes call.

	if err := grpcSrv.Listen(ctx); err != nil {
		return err
	}

	httpSrv, err := buildHTTP(ctx, cfg, log, healthRegistry, metricsRegistry)
	if err != nil {
		return err
	}

	// Registration order is shutdown order reversed: dependencies first, transports last,
	// so the transports drain before what they depend on closes.
	runner := bootstrap.NewRunner(log, cfg.ShutdownTimeout)

	runner.Add(bootstrap.Component{
		Name: "tracing",
		Stop: shutdownTracing,
	})
	runner.Add(bootstrap.Component{
		Name: "database",
		Stop: func(context.Context) error { db.Close(); return nil },
	})
	runner.Add(bootstrap.Component{
		Name: "cache",
		Stop: func(context.Context) error { return redis.Close() },
	})
	runner.Add(bootstrap.Component{
		Name: "broker",
		Stop: func(context.Context) error { return broker.Close() },
	})
	runner.Add(bootstrap.Component{
		Name:  "outbox-worker",
		Start: outboxWorker.Run,
	})
	runner.Add(bootstrap.Component{
		Name:  "grpc",
		Start: func(ctx context.Context) error { return grpcSrv.Serve(ctx) },
		Stop:  grpcSrv.Shutdown,
	})
	runner.Add(bootstrap.Component{
		Name:  "http",
		Start: func(ctx context.Context) error { return httpSrv.Serve(ctx) },
		Stop:  httpSrv.Shutdown,
	})

	log.Info().
		Str("grpc", grpcSrv.Address()).
		Str("http", httpSrv.Address()).
		Msg("ready")

	return runner.Run(ctx)
}

// buildGRPC assembles the gRPC server with the interceptor chain of 03 section 7.
func buildGRPC(cfg *config.Config, log zerolog.Logger, m *metrics.Registry) (*grpcserver.Server, error) {
	return grpcserver.New(grpcserver.Config{
		Address:             cfg.GRPCAddress(),
		Environment:         cfg.Environment,
		Logger:              log,
		Metrics:             m,
		MaxRecvMsgSize:      cfg.GRPCMaxRecvMsgSize,
		ShutdownTimeout:     cfg.ShutdownTimeout,
		MinClientAPIVersion: cfg.MinClientAPIVersion,
		// Nothing is public by default (03 section 7). HealthPing is the reference RPC
		// of RF-19 and the healthchecks back the probes.
		PublicMethods: interceptor.NewAllowlist(interceptor.HealthMethods()...),
	}, grpcserver.Dependencies{
		// RateLimiter, Authenticator and Authorizer arrive with RF-10, RF-14 and PRD-14.
		// A nil dependency is skipped rather than replaced by a permissive stub.
	})
}

// buildHTTP assembles the REST gateway and the operational routes.
func buildHTTP(
	ctx context.Context,
	cfg *config.Config,
	log zerolog.Logger,
	healthRegistry *health.Registry,
	metricsRegistry *metrics.Registry,
) (*httpserver.Server, error) {
	// No REST gateway yet: this service has no RPCs of its own until its PRD adds them.
	// The operational routes -- /livez, /readyz, /metrics -- are served regardless, which
	// is what the probes and Prometheus need.
	srv, err := httpserver.New(httpserver.Config{
		Address:         cfg.HTTPAddress(),
		ServiceName:     cfg.ServiceName,
		Version:         version.Get().Version,
		Logger:          log,
		Gateway:         nil,
		Health:          healthRegistry,
		Metrics:         metricsRegistry.Prometheus(),
		ShutdownTimeout: cfg.ShutdownTimeout,
		// Only identity publishes the key set; this service carries the public keys in
		// configuration and validates with no network call.
	})
	if err != nil {
		return nil, err
	}

	if err := srv.Listen(ctx); err != nil {
		return nil, err
	}

	return srv, nil
}
