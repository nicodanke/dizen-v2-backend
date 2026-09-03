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
`staging`. There are no path filters and no change detection: if this repository changed,
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

### What triggers a deployment

Not a git push, and not before the pipeline is green. Every git-based trigger -- a branch watcher, a tag you push by hand --
reacts to the git event, which happens *before* the images for that commit exist; the
deployment would pull `manifest unknown` every time. Only something running after the build
knows they are there, so `images.yml` gates its deploy job on all five images being
published.

The image build and the deploy are jobs of `ci.yml` and they `needs` every verification job,
so nothing is published or deployed from a commit that failed lint, tests or the coverage
gate. They used to live in a separate workflow, which meant they ran in parallel with the
tests and could deploy a red commit -- the opposite of what acceptance criterion 1 asks for.

| Event | Tag the workflow pushes | Dokploy pattern |
|---|---|---|
| push to `staging` | `deployed-staging-<sha>` | `deployed-staging-*` |
| tag `v1.2.3` | `deployed-v1.2.3` | `deployed-v*` |

The two patterns do not overlap, so one environment cannot pick up the other's tag. Because
the trigger is a ref rather than a URL there is no unauthenticated webhook and no secret to
keep, and the deployment leaves a trace in the history saying which commit went out and when.

Note the second tag on a release: `v1.2.3` is pushed by a person and exists before the images
do, so it cannot be the trigger. `deployed-v1.2.3` is pushed by the workflow once they exist.

**Dokploy's own branch auto-deploy must be off**, on all three projects. On the application
it is the trigger that races the build; on Redis and RabbitMQ it would recreate the broker
and the cache on every unrelated commit.

### The other two workflows

| Workflow | Fires on | Does |
|---|---|---|
| `publish-contract` | a push to `main` touching `proto/` | tags `api-vX.Y.Z`, publishes the OpenAPI as a release artifact, and asks `dizen-v2-mobile` and `dizen-v2-web` for their bump pull request |
| `release` | a `v*` tag on `main` | changelog and GitHub release. It does NOT deploy: the same tag also runs `ci.yml`, which builds the images and then pushes `deployed-v1.2.3`, and that is what Dokploy reacts to |

The contract version is independent of the version of the services: `v1.4.0` is a release of
this backend, `api-v1.4.0` is a state of the API that the two clients pin themselves to. The
bump is the minor one by default; `scripts/next-api-version.sh` prints what the next tag
would be, and a commit reaching `main` can override it with an `api-release: major` or
`api-release: patch` trailer.

**The backend deploys first, always** (01 section 3.2): contract changes are additive, so a
published app keeps working against a newer backend, and the other way around is not
guaranteed. The release workflow depends on neither of the other two repositories.

### Branches and secrets

`main` is the trunk and `staging` is what the staging environment runs; work happens in
`feature/*` and `hotfix/*`. The deployment branch is named after the environment it deploys
because the mapping is exactly one to one and CI enforces it -- `main` keeps its name because
production is cut by a `v*` tag, not by pushing to it (`D-34`).
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
one file serves both environments: `dizen-v2-staging` and `dizen-v2-production` are two
separate Dokploy projects with their own domains, their own databases and their own secrets,
sharing nothing. Everything that differs is a variable.

```bash
make deploy-check   # the compose parses and every variable it reads is documented
```

That check is in CI. Compose only *warns* about a variable it cannot resolve and carries on
with an empty string, which on a server is an empty database password or a router with no
host, so the warning is turned into a failure.

### Secrets: Doppler

There is no `.env` on the server and none in this repository.
[`deploy/dokploy.env.example`](deploy/dokploy.env.example) names every variable, empty, and
the values live in Doppler:

```
Doppler project `dizen-backend`
  ├── config `dev`  ->  `doppler run -- ...`      the developer machine
  ├── config `stg`  ->  provider `doppler-stg`    ->  dizen-v2-staging
  └── config `prd`  ->  provider `doppler-prd`    ->  dizen-v2-production
```

Dokploy reads Doppler through a **Service Token** (`dp.st.`), which is read-only and scoped
to one project and one config: a leaked token exposes that config and nothing else. One
provider per config, so staging and production do not even share the token that reads their
secrets. In the Dokploy environment variables each secret is referenced as
`${{vault.doppler-stg.IDENTITY_DATABASE_URL}}`; the non-secret variables are written
literally.

Locally, `make up` runs the compose under `doppler run` when the CLI is configured and falls
back to the committed development defaults when it is not, so a fresh clone still works. What
Doppler is actually protecting on a laptop is the JWT signing key that `make jwt-key`
otherwise writes to a file.

### What runs

Five services and the backup container. **The data stores are not in this compose**: Postgres,
Redis and RabbitMQ are managed outside it and arrive as connection strings, the same way the
v1 stack does it (`D-32`). One database and one credential per service, no cross-access.

One Redis and one RabbitMQ per environment, not one per service: Redis keys are already
namespaced per service, and the broker has a single exchange. [`deploy/rabbitmq/`](deploy/rabbitmq/)
is a compose stack for the broker, deployed as its own Dokploy project — there is nothing to
configure inside it, because the services declare their own exchange, queues, dead letter
queues and bindings at startup.

Two settings on those stores are worth getting right the first time, and both are the kind
that fail quietly:

- **RabbitMQ's memory watermark must be absolute.** Its default is 40% of the memory it can
  see, and in a container that is the memory of the *host* -- which also runs v1. It would
  grow past its own limit and be OOM-killed with nothing in its log to explain it. It is set
  in `deploy/rabbitmq/rabbitmq.conf`, because the image rejects the old environment variable
  and refuses to start.
- **Redis needs `maxmemory-policy noeviction`.** An LRU policy would evict entries of the
  session revocation list, which turns a revoked token back into a valid one -- a silent
  security failure, under pressure, that no test would catch.

Every container carries an explicit CPU and memory limit, which matters more here than it
would on an empty machine: this VPS also runs v1.

### Routing

| | Staging | Production |
|---|---|---|
| REST, all three services | `api.staging.v2.dizen.pro` | `api.v2.dizen.pro` |
| gRPC identity | `grpc-identity.staging.v2.dizen.pro` | `grpc-identity.v2.dizen.pro` |
| gRPC tours | `grpc-tours.staging.v2.dizen.pro` | `grpc-tours.v2.dizen.pro` |
| gRPC booking | `grpc-booking.staging.v2.dizen.pro` | `grpc-booking.v2.dizen.pro` |
| Dashboard API | `admin-api.staging.v2.dizen.pro` | `admin-api.v2.dizen.pro` |

v2 lives under its own subdomain because **v1 already owns `api.dizen.pro` and
`grpc-*.dizen.pro`** and cannot be turned off yet (`D-31`). The cutover, when v1 is retired,
is a DNS and host-rule change and nothing else.

REST shares one host and is routed by path, resting on a convention: **the first segment
after `/v1/` carries the service name** (`D-24`), so the routing is a constant rather than a
table that grows with every RPC. Only `/v1/` is published; `/livez`, `/readyz` and `/metrics`
answer on the same port and are reachable only from inside.

**gRPC gets a host per service** (`D-38`), and that is not symmetry for its own sake. Routing
one shared gRPC host by proto package made reflection impossible: a reflection call travels
under `/grpc.reflection...` and does not say whose schema it wants, so there is nothing to
route on -- Traefik answered 404 and the client reported it as an unreadable framing error. A
host each also drops the requirement that every proto package be named after its service, a
convention that had to be remembered on each new file and broke silently when it was not.
It is also what the v1 stack already does, so the mobile app is not learning a new pattern.

`scheme=h2c` on the gRPC services is the line this kind of deployment most often gets wrong:
without it Traefik downgrades the internal leg to HTTP/1.1 and every call dies with an
unreadable framing error. The v1 stack sets the same label for the same reason. Verify it
against the real domain rather than by hand:

```bash
make grpc-check HOST=grpc-identity.staging.v2.dizen.pro:443
```

### Living next to v1

Both stacks share one Traefik and one `dokploy-network`, so every name that Traefik sees is
prefixed with the environment and with `v2`: `staging-v2-identity-rest`, never
`identity-rest`. Two routers with the same name overwrite each other silently, and the one
that loses is whichever Docker reports second.

| | v1 | v2 | Collide? |
|---|---|---|---|
| Hosts | `api.dizen.pro`, `grpc-*.dizen.pro` | `*.v2.dizen.pro` | no |
| Traefik routers | `user-prod`, `booking-staging` | `production-v2-identity-rest` | no |
| Container names | explicit (`user-service-prod`) | generated (`dizen-v2-staging-identity-1`) | no |
| Published ports | none | none | no |
| Networks | `dokploy-network` | `dokploy-network` **plus** a private `internal` | no |

The private network is the one real improvement over v1: v2 service-to-service gRPC and
Valhalla never touch the shared network, so they are not reachable from the v1 containers.
`internal` is per project, so staging and production get one each.

### Bringing it up

**Staging first, and it is not a formality**: it is the same compose, the same image build and
the same Traefik that production will use, so anything structurally wrong shows up there.

1. Create the Doppler configs and the Dokploy provider, and the databases outside the compose.
2. Point the DNS of the three staging hosts at the VPS.
3. Create the `dizen-v2-staging` project in Dokploy, pointed at this repository and
   `deploy/docker-compose.prod.yml`, with `DIZEN_ENV=staging`.
4. Deploy, and check in this order: the containers are `ready` (migrations applied, `RF-7`),
   `/readyz` answers through the internal network, the REST host answers over TLS, and
   `grpcurl` answers on the gRPC host (acceptance criterion 3).
5. Watch what the VPS actually costs with both stacks up, and adjust the limits before
   repeating the four steps for production with `DIZEN_ENV=production`.

**The backend deploys before the apps, always** (`01` section 3.2): contract changes are
additive, so a published app keeps working against a newer backend, and the reverse is not
guaranteed.

### Known gap

The rolling update of `RNF-2` is **not** achieved by plain Compose: `docker compose up`
recreates a container rather than holding the old one until the new one answers `/readyz`, so
a deployment has a short window of refused connections. Closing it needs either Dokploy's
Swarm-backed application deployments or a second replica behind Traefik. It is written down
here rather than assumed, and tracked as decision `D-25`.

## Hard rules

They live in [`CLAUDE.md`](CLAUDE.md) and are not preferences: sqlc and never an ORM, one
database per service with no cross-access, generated code is committed, proto changes are
additive, coverage >= 70% is blocking, no secrets in the repository, a deadline-carrying
context on every outbound call, and everything written in English.
