# Cheat sheet

Every command runs from the repository root. `make help` lists them all with a one-line
description; this page is the same list with the context that makes it useful.

---

## First run

Three commands from a clean clone:

```bash
make tools     # install the pinned tools into ./bin      (~3 min the first time)
make doctor    # check everything needed is installed and running
make up        # bring up the whole local environment     (~2 min the first time)
```

`make up` does not return until all five services answer `200` on `/livez` and `/readyz`.
If it finishes, the environment works.

Optionally, `make seed` creates the MinIO bucket and loads sample data.

### What you need installed

| | | |
|---|---|---|
| **Go 1.27+** | required | With the default `GOTOOLCHAIN=auto` any Go will do: it downloads 1.27 on its own |
| **Docker** | required | Running, for the environment and the integration tests |
| **git** | required | |
| **Dart SDK** | optional | Only for `make proto`. `make tools` warns and carries on without it |

### When something is wrong

```bash
make doctor
```

It checks the toolchain, the Docker daemon, the pinned tools against `tools/versions.mk`,
port conflicts, and whether each service answers `/readyz`. It reports what is missing and
the command that fixes it, so "it does not build" becomes a specific thing to install.

---

## The local environment

```bash
make up                      # build and start everything, wait until it answers
make down                    # stop it, keeping the data
make down-clean              # stop it and delete the data
make ps                      # what is running
make logs                    # follow all the logs
make logs SERVICE=identity   # follow one service
make restart SERVICE=tours   # restart one service
make seed                    # sample data and the MinIO bucket
```

**Does the data survive?** Yes. Each database lives in a named Docker volume:

| Command | Containers | Data |
|---|---|---|
| `make down` | removed | **kept** |
| `make down-clean` | removed | **deleted** |

The everyday cycle — `make down` at night, `make up` in the morning — keeps every row.
`make down-clean` is the deliberate reset; migrations run at startup, so the schema rebuilds
itself.

**Hot reload** is on: save a `.go` file and Air recompiles. The mount covers `pkg/` too, so a
change to the shared library reloads all five services.

### Where things are

| | |
|---|---|
| identity | http://localhost:8081 · gRPC on 9091 |
| tours | http://localhost:8082 · gRPC on 9092 |
| booking | http://localhost:8083 · gRPC on 9093 |
| admin | http://localhost:8084 · gRPC on 9094 |
| mail-dispatcher | http://localhost:8085 · no public API |
| Traefik dashboard | http://localhost:8090 |
| RabbitMQ panel | http://localhost:15672 · dizen / dizen |
| MinIO console | http://localhost:9001 · dizen / dizen12345 |
| Prometheus | http://localhost:9090 |
| Grafana | http://localhost:3000 · dizen / dizen |
| Jaeger | http://localhost:16686 |

Every service also serves `/livez`, `/readyz` and `/metrics`. `/readyz` answers `503` naming
the dependency that is failing, which is the fastest way to tell "the service is down" from
"its database is".

---

## Writing code

```bash
make build     # compile the six modules (binaries in dist/)
make test      # unit tests, fast, no Docker needed
make lint      # golangci-lint, must be clean
make fmt       # gofumpt + goimports
make vet       # go vet
make fix       # apply the modernizations go fix suggests
```

`make fmt` runs gofumpt, a strict superset of gofmt, so plain `go fmt` is already covered.
`make fix` is a different tool: since Go 1.27 it rewrites code to modern language features.
Its analyzers also run inside `make lint`, so stale constructs block CI.

### Verification mode, for CI

The same checks without modifying anything. If any of them fails, the build fails rather
than letting the problem through:

```bash
make fmt-check   # gofumpt and goimports would change nothing
make fix-check   # go fix has nothing left to modernize
```

### Which commands need `./bin`

| Work without `make tools` | Need `make tools` |
|---|---|
| `build` `test` `vet` `up` `down` `logs` | `lint` `fmt` `proto` `sqlc` `migrate-*` `mocks` `secrets-scan` |

---

## Tests

```bash
make test               # unit only, seconds, no Docker
make test-integration   # with testcontainers, needs Docker
make test-coverage      # everything plus the 70% gate    (~5 min)
make coverage-html      # open the report in a browser
```

Integration tests live behind the `integration` build tag, which is why `make test` stays
fast. `make test-coverage` includes them, because that is where most of the repository and
transport coverage comes from.

The gate is **70%**, blocking, measured with `-coverpkg=./...` so a test in one package gets
credit for the code it exercises in another. Generated code is excluded.

---

## The contract (protos)

```bash
make proto            # regenerate Go, gateway, OpenAPI and the Dart package
make proto-lint       # buf lint
make proto-format     # buf format
make proto-breaking   # compatibility against main; blocks the merge
make proto-check      # fail if make proto leaves an uncommitted diff
```

**Generated code is committed** so no build depends on having the tools installed. A change
to a `.proto` is not finished until `make proto` has run and the result is committed.

Contract changes are **always additive**: a field number is never reused, and `buf breaking`
enforces it.

---

## Database

```bash
make sqlc                                       # regenerate the typed queries
make sqlc-check                                 # fail if the output is stale
make sqlc-vet                                   # sqlc's own checks over the queries
make migrate-up SERVICE=tours
make migrate-down SERVICE=tours
make migrate-create SERVICE=tours NAME=add_x
make migrate-version SERVICE=tours
```

In the containers migrations are applied at startup, never by hand: a schema that depends on
somebody remembering to run a command is a schema that drifts between environments. The
`migrate-*` targets are for development.

---

## API collection

```bash
make api-client   # validate the versioned Yaak collection
```

The collection lives in [`api-client/`](api-client/) and is synced by Yaak's Directory Sync.
**A new endpoint is not finished until it has its request there**, the same way it is not
finished without a test.

The `Local` environment comes filled in with the ports the compose file publishes, so the
requests work right after `make up`. `Staging` and `Production` are versioned empty, and the
token variables are empty everywhere — they are filled in at run time by the sign-in request.
`make api-client` enforces exactly that.

---

## Continuous integration

The pipeline lives in `.github/workflows/` and every job runs a `make` target, so what
fails in CI can be reproduced with one command locally.

```bash
make secrets-scan   # gitleaks over the working tree and the history
make commit-check   # Conventional Commits on this branch (RANGE= overrides)
```

| CI job | What it runs | Locally |
|---|---|---|
| `static analysis` | format, `go vet`, golangci-lint, `go fix`, the Yaak collection | `make fmt-check vet lint fix-check api-client` |
| `commits` | Conventional Commits, on the branch and on the PR title | `make commit-check` |
| `contract` | `buf lint`, `buf format`, `buf breaking` against main, generated code without diff | `make proto-lint proto-breaking proto-check` |
| `generated queries` | the sqlc output matches the queries | `make sqlc-check` |
| `secrets` | gitleaks, blocking | `make secrets-scan` |
| `unit tests` | `go test -race`, no Docker | `make test` |
| `coverage gate` | integration tests and the 70% threshold | `make test-coverage` |

`make test-coverage` runs the modules in parallel, since what the suite spends its time on is
starting containers rather than running tests. `COVERAGE_JOBS` bounds how many run at once
(CI sets 2) and `COVERAGE_SEQUENTIAL=1` forces one at a time when the interleaved output is
hiding something.

Integration tests share one container per package: `TestMain` starts it, migrates it and
snapshots it, and `liveRepo`/`newStack` restore that snapshot after every test. **A new
integration test gets an empty, migrated schema for free** -- and must not call `t.Parallel()`,
because the container is shared.

`static analysis` also runs `make deploy-check`, which parses
`deploy/docker-compose.prod.yml` and fails if it reads a variable that
`deploy/dokploy.env.example` does not document. Compose only warns about a missing variable
and carries on with an empty string, and on a server that is an empty database password.

The other two workflows are not run by hand: `publish-contract` tags `api-vX.Y.Z` when a
change to `proto/` reaches `main`, and `release` deploys to production when a `v*` tag is
created. `scripts/next-api-version.sh` prints what the next contract tag would be, which is
useful for checking before merging.

```bash
scripts/next-api-version.sh          # the next api-v tag, minor bump by default
scripts/branch-protection.sh --dry-run   # the protection the two branches should have
```

---

## Deployment

`deploy/docker-compose.prod.yml` is what Dokploy runs, in two separate projects
(`dizen-v2-staging`, `dizen-v2-production`) that share no domain, no database and no secret.
`deploy/dokploy.env.example` is the inventory of variables, versioned empty; the values live
in Doppler.

```bash
make deploy-check    # the compose parses and every variable it reads is documented
```

| | Staging | Production |
|---|---|---|
| Deploys from | push to `staging` | tag `v*` on `main` |
| Hosts | `*.staging.v2.dizen.pro` | `*.v2.dizen.pro` |
| Basic authentication at the edge | yes, on the REST hosts | no |
| Valhalla and planetiler | yes, behind the `authoring` and `tiles` profiles | no |
| Trace sampling | 1.0 | a fraction |

The data stores are **not** in this compose: Postgres, Redis and RabbitMQ are managed outside
it and arrive as connection strings, one per service.

Four things to know before touching it:

- **v2 runs next to v1**, which owns `api.dizen.pro`. Every Traefik name carries the
  environment and `v2` (`staging-v2-identity-rest`) because both stacks share one Traefik,
  and two routers with the same name overwrite each other in silence.
- **A bcrypt hash in `BASIC_AUTH_USERS` needs every `$` doubled.** `$` is what Compose
  interpolates, so an unescaped hash arrives mutilated and the login never works.
- **The gRPC services need `scheme=h2c`.** Without it Traefik downgrades the internal leg to
  HTTP/1.1 and every gRPC call fails with an unreadable framing error.
- **Secrets come from Doppler**, referenced in Dokploy as `${{vault.doppler-stg.NAME}}`.
  Locally `make up` runs under `doppler run` when the CLI is configured.

```bash
grpcurl -d '{}' grpc.staging.v2.dizen.pro:443 dizen.identity.v1.HealthService/HealthPing
```

---

## Other

```bash
make mocks     # regenerate the mocks over the repository interfaces
make jwt-key   # generate an Ed25519 key pair for development
make tools-dart # install protoc-gen-dart on its own, after installing the Dart SDK
make tidy      # tidy the dependencies of every module
make clean     # remove ./bin, dist/ and the coverage artifacts
make help      # every target with its description
```

---

## Before opening a pull request

```bash
make fmt && make fix && make lint && make test-coverage
make proto-check && make sqlc-check && make secrets-scan
```

The last three need an initialized git repository, since they compare against what is
committed.
