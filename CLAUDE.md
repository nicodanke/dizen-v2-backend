# dizen-v2-backend

Backend for **Dizen**, an app for self-guided audio tours driven by geolocation. Go microservices
over gRPC, and this repository is also **the owner of the API contract**: the protos live here, and
the clients consumed by the Flutter app and the web dashboard are published from here.

Root module: `github.com/nicodanke/dizen-v2-backend`

---

## Where the documentation lives

In the product repository, cloned **as a sibling folder** of this one:

```
../dizen-product/
├── docs/
│   ├── 00-vision-producto.md          what the product is and who it is for
│   ├── 01-arquitectura-tecnica.md     services, repos, communication, security  <- ALWAYS READ
│   ├── 02-modelo-de-datos.md          schema of the five databases              <- ALWAYS READ
│   ├── 03-api-contratos-grpc.md       protos, REST mapping, errors, interceptors <- ALWAYS READ
│   ├── 04-motor-geo-audio.md          the audio triggering engine (tours context)
│   ├── 05-roadmap-y-dependencias.md   build order and open decisions
│   ├── 07-mapas-rutas-y-navegacion.md routing with Valhalla, no Google APIs
│   └── prd/fase-1/PRD-XX-*.md         25 PRDs: the specification of each piece
└── design-tokens/                     not applicable to the backend
```

The documentation is written in Spanish and stays that way; it is the only exception to hard rule 8.

**Before writing code for a PRD, read that PRD in full plus documents `01`, `02` and `03`.**
That is the minimum context. Do not improvise anything that is already specified there.

## How we work

- **One `RF-x` per iteration.** Each PRD numbers its functional requirements; each one is a unit of
  work. Do not try to implement a whole PRD in one go: the result is plausible and shallow.
- **The PRD acceptance criteria (section 6) are the definition of done.** If an `RF` has no test
  verifying its criterion, it is not done.
- **If the PRD and the real code contradict each other, say so before improvising.** The PRD may be
  out of date and need fixing, or there may be a reason. Ask.
- **Every new architecture decision is recorded** in
  `../dizen-product/docs/05-roadmap-y-dependencias.md` section 5. Decisions already taken are in
  `01` section 9 as ADRs; do not reverse them without discussion.

## Hard rules

These are not preferences, they are decisions already taken and documented:

1. **sqlc, never an ORM.** Explicit SQL in `db/queries/*.sql`, generated types. No GORM.
   Migrations with `golang-migrate`; sqlc only reads the schema.
2. **One database per service, no cross-access.** No joins across databases, no connection to
   another service's database "just for a report". Data from another domain is requested over gRPC.
3. **Generated code is committed**: `pkg/genproto/`, `db/sqlc/`, `gen/dart/`, `gen/openapi/`. CI
   fails if `make proto` or `make sqlc` produce a diff.
4. **Proto changes are always additive.** `buf breaking` runs against `main` and blocks the merge. A
   field number is never reused. If a break is unavoidable, a `v2` is created and both coexist.
5. **Coverage >= 70% across the whole repo, blocking.** Measured with
   `go test ./... -coverpkg=./... -covermode=atomic`. Without `-coverpkg`, the coverage that
   integration tests give to handlers and repositories is not counted. `pkg/genproto`, `db/sqlc`,
   mocks and `*.pb.go` are excluded.
6. **No secrets in the repository.** Only a commented `.env.example`.
7. **A deadline-carrying context on every outbound call**, propagating the deadline received.
8. **Language: everything in this repository is written in English.** Identifiers, table names,
   fields, RPCs, comments, documentation, commit messages and the text the tooling emits. The same
   applies to `dizen-v2-mobile` and `dizen-v2-web`. The only exception is `dizen-product`, which is
   product documentation and stays in Spanish.

## Layout

```
dizen-v2-backend/
├── go.work                  workspace: pkg/ and the five services
├── proto/                   single source of the contract (buf)
├── gen/                     published artifacts: dart/dizen_api, openapi
├── pkg/                     shared library (its own module)
├── services/
│   ├── identity/            app users, login, JWT, sessions
│   ├── tours/               destinations, tours, variants, nodes, media, runs
│   ├── booking/             bookings, entitlements, payments
│   ├── admin/               administrators, RBAC, auditing, dashboard BFF
│   └── mail-dispatcher/     event consumer: emails and push
└── deploy/                  development and production compose, observability
```

Each service: `cmd/server/main.go` plus `internal/{config,service,repository,db,transports}/`.
`main.go` is pure composition: read config -> open dependencies -> build services -> start
transports -> wait for a signal -> shut down gracefully.

## Commands

```bash
make doctor          # check the toolchain, Docker, the pinned tools and the environment
make up              # bring up the whole local environment (databases, redis, rabbit, minio, services)
make down            # stop it, keeping the data
make down-clean      # stop it and delete the data
make proto           # regenerate Go, gateway, OpenAPI and the Dart package
make sqlc            # regenerate the typed queries
make migrate-up SERVICE=tours
make test            # unit tests
make test-integration# with testcontainers
make test-coverage   # with the 70% gate
make lint
make api-client      # validate the versioned Yaak collection
```

`make down` keeps the database volumes; only `make down-clean` deletes them.

The full list, with the context behind each command, is in `CHEATSHEET.md`.

## What not to do

- Add endpoints that are not in `03-api-contratos-grpc.md` without updating it first.
- Use a Google Maps API or a paid routing API: the route is stored as content and authoring-time
  routing is self-hosted Valhalla (`07-mapas-rutas-y-navegacion.md`).
- Put another domain's business logic into `admin`: that service only aggregates and authorizes.
- Lower the coverage threshold in the configuration to make the build pass.
