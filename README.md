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

Everything else -- buf, sqlc, mockery, migrate, golangci-lint and the protoc plugins -- is
installed pinned into `./bin` by `make tools`. Versions live in
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

## Hard rules

They live in [`CLAUDE.md`](CLAUDE.md) and are not preferences: sqlc and never an ORM, one
database per service with no cross-access, generated code is committed, proto changes are
additive, coverage >= 70% is blocking, no secrets in the repository, a deadline-carrying
context on every outbound call, and everything written in English.
