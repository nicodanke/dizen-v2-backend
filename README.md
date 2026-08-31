# dizen-v2-backend

Backend for **Dizen**, an app for self-guided audio tours driven by geolocation. Go
microservices over gRPC, and the **owner of the API contract**: the protos live here, and
the clients consumed by `dizen-v2-mobile` and `dizen-v2-web` are published from here.

Product and architecture documentation lives outside this repository, in
[`../dizen-product/docs`](../dizen-product/docs) (in Spanish). The required reading is
`01-arquitectura-tecnica.md`, `02-modelo-de-datos.md` and `03-api-contratos-grpc.md`.

## Requirements

| | |
|---|---|
| Go | 1.27+ (the toolchain downloads itself with `GOTOOLCHAIN=auto`) |
| Docker | for the local environment and the integration tests |
| Dart SDK | only to regenerate the `gen/dart/dizen_api` package |

Everything else -- buf, sqlc, mockery, migrate, golangci-lint, gitleaks and the protoc
plugins -- is installed pinned into `./bin` by `make tools`. Versions live in
[`tools/versions.mk`](tools/versions.mk), which is the single source of truth.

## Getting started

From a clean clone, three commands:

```bash
make tools     # install the pinned tools into ./bin  (~3 min the first time)
make doctor    # check everything needed is installed and running
make up        # bring up the whole local environment (~2 min the first time)
```

`make up` does not return until all five services answer `200` on `/livez` and `/readyz`, so
if it finishes, the environment works. Add `make seed` for the sample data.

**[`CHEATSHEET.md`](CHEATSHEET.md) has every command with the context that makes it useful.**
`make help` lists them without leaving the terminal.

`make doctor` is the command to run first on a new machine, and the first one to run when
something stops working: it turns "it does not build" into a specific missing thing plus the
command that installs it. It checks the toolchain, the Docker daemon, the pinned tools
against `tools/versions.mk`, port conflicts, and whether each service answers `/readyz`.

```bash
make build     # build the six modules of the workspace
make test      # unit tests
make lint      # golangci-lint, no warnings (RNF-4)
make fmt       # gofumpt + goimports
make fix       # apply the modernizations `go fix` suggests
```

`make fmt` runs gofumpt, a strict superset of gofmt, so plain `go fmt` is already covered.
`go fix` is a different tool: since Go 1.27 it rewrites code to modern language and library
features. Its analyzers also run inside `make lint` (the `modernize` and `intrange`
linters), so stale constructs block CI; `make fix` is the convenience that applies them.

## Layout

```
go.work            workspace: the root module, pkg/ and the five services
proto/             single source of the contract (buf)
gen/               published artifacts: dart/dizen_api, openapi
pkg/               shared library (its own module)
services/          identity, tours, booking, admin, mail-dispatcher
deploy/            development and production compose, observability
```

`pkg/` and each service are independent Go modules joined by `go.work`. **There are no
`replace` directives**: resolution is handled by the workspace `use` block (01 section 3),
which is why every build -- Dockerfiles included -- copies the root `go.work`.

`go build ./...` from the root does not cross module boundaries; the equivalent is
`make build`, which iterates the workspace modules through
`scripts/for-each-module.sh`.

## The local environment

`make up` builds and starts everything: five Postgres (one per service), Redis, RabbitMQ,
MinIO, the five services with hot reload, Prometheus, Grafana, Jaeger and Traefik. It does
not return until all five answer `200` on `/livez` and `/readyz`, so a `make up` that
finishes is an environment that works. From cold it takes under two minutes.

| | |
|---|---|
| Traefik dashboard | http://localhost:8090 |
| RabbitMQ panel | http://localhost:15672 (dizen / dizen) |
| MinIO console | http://localhost:9001 (dizen / dizen12345) |
| Prometheus | http://localhost:9090 |
| Grafana | http://localhost:3000 (dizen / dizen) |
| Jaeger | http://localhost:16686 |
| identity | http://localhost:8081, gRPC on 9091 |
| tours | http://localhost:8082, gRPC on 9092 |
| booking | http://localhost:8083, gRPC on 9093 |
| admin | http://localhost:8084, gRPC on 9094 |
| mail-dispatcher | http://localhost:8085, no public API |

### Does the data survive a restart?

Yes. Each database lives in a named Docker volume, so the two teardown commands differ in
exactly this:

| Command | Containers | Data |
|---|---|---|
| `make down` | removed | **kept** |
| `make down-clean` | removed | **deleted** |

So the everyday cycle — `make down`, then `make up` the next morning — keeps every row.
`make down-clean` is the deliberate reset, for when a migration left the schema in a state
not worth repairing. Migrations are applied at startup, so a fresh environment rebuilds the
schema on its own.

## The contract

The `.proto` files under `proto/` are the single source of truth. `make proto` regenerates,
in this order:

| Output | Consumer |
|---|---|
| `pkg/genproto/` | Go: gRPC and gateway. Consumed directly, in-repo |
| `gen/openapi/<service>.yaml` | OpenAPI 3.0.3, one per service, for `dizen-v2-web` |
| `gen/dart/dizen_api/` | Dart package pinned by tag, for `dizen-v2-mobile` |

**All generated code is committed** so that no build depends on having the tools
installed. `make proto-check` fails if `make proto` leaves a diff behind, and that is what
CI runs.

```bash
make proto            # regenerate all three outputs
make proto-lint       # buf lint
make proto-breaking   # buf breaking against main; blocks the merge
make proto-check      # fail if the generated code is stale
```

Contract changes are **always additive**: a field number is never reused, and `buf
breaking` enforces it. When a break is unavoidable, a `v2` is created and both coexist
(01 section 3.2).

## Continuous integration

`.github/workflows/ci.yml` runs on every pull request and on every push to `main` and
`develop`. There are no path filters and no change detection: if this repository changed,
the whole pipeline runs. That is the concrete advantage of three separate repositories -- a
push to `dizen-v2-mobile` costs nothing here.

| Job | What it verifies |
|---|---|
| `static analysis` | gofumpt, `go vet`, golangci-lint, `go fix` applied, tidy modules, the Yaak collection without credentials, the production compose |
| `commits` | the commit messages and the pull request title follow Conventional Commits |
| `contract` | `buf lint`, `buf format`, `buf breaking` against `main`, and `make proto` without a diff |
| `generated queries` | `make sqlc` without a diff |
| `secrets` | gitleaks over the working tree **and** the history, blocking |
| `unit tests` | `go test -race`, no Docker |
| `coverage gate` | integration tests with testcontainers and the 70% threshold |

Every job is a `make` target, so a red build is reproduced locally with one command; the
mapping is in [`CHEATSHEET.md`](CHEATSHEET.md).

A run superseded by a newer push to the same branch is cancelled, and it ends mid-step with
`Error: The operation was canceled.` -- which reads exactly like a failure and is not one.
If a run stopped partway through and there is a newer run behind it, that is the
`concurrency` block doing its job.

The coverage gate is the long pole, and what it spends its time on is starting containers
rather than running tests: every module costs about the same regardless of how much code it
has. Two things address that, and only together were they enough:

- **One container per package, not per test.** `TestMain` starts one PostgreSQL, migrates it
  and takes a snapshot; every test gets that snapshot restored on cleanup, in milliseconds.
  This took the suite from about seventy container startups to eighteen (`D-30`).
- **The modules run in parallel**, bounded by `COVERAGE_JOBS`.

The twelve startups that remain are in `pkg/database`, whose tests are about the database
lifecycle itself -- connecting, migrating, failing to migrate -- and so want a fresh
container. That is the next lever if it is ever needed. The coverage report is uploaded as an
artifact of each run, including when the gate fails, which is when somebody actually needs
to read it. The comment on the pull request and the README badges arrive with `PRD-25`,
once decision `D-17` settles which tool publishes them.

```bash
make secrets-scan   # the same gitleaks check CI runs
```

### Commit messages

Conventional Commits, enforced. It is not a new convention: `PRD-25` RF-14 generates the
changelog from the commits, so the format was already assumed -- and a convention nothing
enforces is one that half the history does not follow, which makes the changelog a list with
holes in it.

```
type(scope)!: subject
```

| | |
|---|---|
| type | `build` `chore` `ci` `docs` `feat` `fix` `perf` `refactor` `revert` `style` `test` |
| scope | optional, lowercase, free text: `tours`, `pkg/amqp`, `deploy` |
| `!` | optional, marks a breaking change -- the only marker that survives a squash |
| subject | required, lowercase, no trailing period, header at most 72 characters |

Both the **commits of the branch** and the **pull request title** are checked, because which
of the two reaches `main` depends on the merge button. Merge and revert commits are exempt:
they are written by git and by GitHub, not by a person.

```bash
make commit-check                    # this branch against main
make commit-check RANGE=HEAD         # the whole history
```

### The other two workflows

| Workflow | Fires on | Does |
|---|---|---|
| `publish-contract` | a push to `main` touching `proto/` | tags `api-vX.Y.Z`, publishes the OpenAPI as a release artifact, and asks `dizen-v2-mobile` and `dizen-v2-web` for their bump pull request |
| `release` | a `v*` tag on `main` | changelog, GitHub release, and the production deployment webhook of Dokploy |

The contract version is independent of the version of the services: `v1.4.0` is a release of
this backend, `api-v1.4.0` is a state of the API that the two clients pin themselves to. The
bump is the minor one by default; `scripts/next-api-version.sh` prints what the next tag
would be, and a commit reaching `main` can override it with an `api-release: major` or
`api-release: patch` trailer.

**The backend deploys first, always** (01 section 3.2): contract changes are additive, so a
published app keeps working against a newer backend, and the other way around is not
guaranteed. The release workflow depends on neither of the other two repositories.

### Branches and secrets

`main` is production and `develop` is staging; work happens in `feature/*` and `hotfix/*`.
Both protected branches require the six jobs above as status checks --
`scripts/branch-protection.sh` applies that with the GitHub CLI, and `--dry-run` shows what
it would send without touching anything.

| Secret | Used by | Missing it means |
|---|---|---|
| `DOKPLOY_PRODUCTION_WEBHOOK_URL` | `release` | the release fails: publishing notes without deploying is worse than failing |
| `CONTRACT_DISPATCH_TOKEN` | `publish-contract` | the contract is still published; the clients bump by hand |
| `TELEGRAM_BOT_TOKEN`, `TELEGRAM_CHAT_ID` | `release` | no notification, nothing else changes |

Each repository holds its own secrets and only its own: this one never sees the store
signing credentials, and `dizen-v2-mobile` never sees a database URL (`PRD-18` RF-11b).

## Deployment

[`deploy/docker-compose.prod.yml`](deploy/docker-compose.prod.yml) is what Dokploy runs, and
one file serves both environments: `dizen-staging` and `dizen-production` are two separate
Dokploy projects with their own databases, domains and variables, and nothing shared between
them. Everything that differs is a variable;
[`deploy/dokploy.env.example`](deploy/dokploy.env.example) names every one of them, empty,
and the values live only in Dokploy (hard rule 6).

```bash
make deploy-check   # the compose parses and every variable it reads is documented
```

That check is in CI. Compose only *warns* about a variable it cannot resolve and carries on
with an empty string, which on a server is an empty database password or a router with no
host, so the warning is turned into a failure.

### What runs

Five services, five Postgres (one per service, never shared), Redis and RabbitMQ. The
databases publish no ports and are not on the network Traefik can see: they are reachable
only from the private bridge, and administrative access is over an SSH tunnel. Every
container carries an explicit CPU and memory limit so one service cannot starve the rest.

### Routing

| Host | Goes to |
|---|---|
| `api.dizen.app` | the REST gateway, path-routed: `/v1/identity/*`, `/v1/tours/*`, `/v1/booking/*` |
| `grpc.dizen.app` | gRPC for the mobile app, routed by proto package, **h2c end to end** |
| `admin-api.dizen.app` | the `admin` service, REST only -- the dashboard does not speak gRPC |

Both rules rest on one convention: **the first segment after `/v1/` and the proto package
both carry the service name**. That is what keeps the routing a constant instead of a table
that grows with every RPC, and every new `google.api.http` annotation has to respect it.
Only `/v1/` is published; `/livez`, `/readyz` and `/metrics` answer on the same port and stay
on the internal network.

`scheme=h2c` on the gRPC services is the line this kind of deployment most often gets wrong:
without it Traefik downgrades the internal leg to HTTP/1.1 and every call dies with an
unreadable framing error. It is verified with `grpcurl` against the public domain, not by
hand:

```bash
grpcurl -d '{}' grpc.dizen.app:443 dizen.identity.v1.HealthService/HealthPing
```

### Authoring: Valhalla and the tiles

Valhalla routes only while a tour is being authored (`07` section 3.2), and `planetiler`
generates the `.pmtiles` of a region. Neither is reachable from the internet -- they are not
on the network Traefik sees -- and both are behind compose profiles, so they run in staging
and not in production: the dashboard is their only user, and a re-import must not compete for
CPU with real traffic.

```bash
docker compose --profile authoring up -d      # Valhalla
docker compose --profile tiles run --rm planetiler   # one-shot, generates a .pmtiles
```

### Backups

A daily dump of the five databases goes to a bucket **outside this server**, keeping 7
daily, 4 weekly and 3 monthly (`RF-9`). It runs in its own container rather than as a
Dokploy database backup, because these databases belong to a compose stack and Dokploy can
only back up what it created.

```bash
docker compose run --rm backup backup    # one run now
docker compose run --rm backup verify    # what is stored
docker compose run --rm backup restore   # lists what can be restored
```

The dumps are in `pg_dump` custom format, so each one is verified as readable before being
uploaded: "the backup ran" and "the backup can be restored" are not the same claim. A run
that fails on any database uploads nothing, because a partial set is worse than none -- it
looks like a backup.

`restore` writes nothing without `--yes`. It drops and recreates every object in the target
database, and the moment it gets run is an incident, which is when a typo is most likely.

**The drill is a command, not a paragraph:**

```bash
make backup-drill
```

It builds the backup image and runs the whole path against real containers -- a database
with data, MinIO standing in for R2, a dump, an upload, a restore into an *empty* database,
and a row-by-row comparison -- plus the retention rules. Run it monthly, which is the
documented restore test `RF-9` asks for, and after any change under `deploy/backup/`. It
touches no real environment: its own network, its own containers, its own bucket, all
removed on exit.

Penpot (`design.dizen.app`) is deployed as its own Dokploy application and is not in this
compose, so it is **not covered yet**: `RF-9` includes it, and losing the design files is as
expensive as losing the data. Adding it is a `DSN_PENPOT` in `BACKUP_DATABASES` plus its
file volume, once that application exists.

### Known gap

The rolling update of `RNF-2` is **not** achieved by plain Compose: `docker compose up`
recreates a container rather than holding the old one until the new one answers `/readyz`,
so a deployment has a short window of refused connections. Closing it needs either Dokploy's
Swarm-backed application deployments or a second replica behind Traefik. It is written down
here rather than assumed, and is tracked as decision `D-25`.

## Hard rules

They live in [`CLAUDE.md`](CLAUDE.md) and are not preferences: sqlc and never an ORM, one
database per service with no cross-access, generated code is committed, proto changes are
additive, coverage >= 70% is blocking, no secrets in the repository, a deadline-carrying
context on every outbound call, and everything written in English.
