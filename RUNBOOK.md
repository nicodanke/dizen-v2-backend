# Runbook

How to operate dizen-v2-backend in staging and production (PRD-18 RF-16).

This is the document to open during an incident, so it states what to type and what the
failure looks like, not how the system is designed. The design is in `README.md`, and the
reasoning behind each decision is in `../dizen-product/docs/05-roadmap-y-dependencias.md`.

| | staging | production |
|---|---|---|
| REST | `https://api.staging.v2.dizen.pro` | `https://api.v2.dizen.pro` |
| Admin API | `https://admin-api.staging.v2.dizen.pro` | `https://admin-api.v2.dizen.pro` |
| gRPC | `grpc-<service>.staging.v2.dizen.pro` | `grpc-<service>.v2.dizen.pro` |
| Branch | `staging` | `main` |
| Image tag | `:staging` | `:production` |
| Dokploy project | `dizen-v2-staging` | `dizen-v2-production` |

---

## Deploy

Nothing is deployed by a git event. The pipeline calls Dokploy's API once the images are
published, which is the only moment the images of that commit exist (D-44).

**Staging.** Push or merge to `staging`. The pipeline verifies, builds, and deploys. About
five minutes end to end.

**Production.** Merge to `main`. The pipeline verifies and builds, then `deploy (production)`
stops and waits: it runs against the `production` GitHub environment, which has required
reviewers. Approve it in the run's page and the deploy proceeds.

Confirm what is actually running — the version is baked into the binary at build time, so it
answers for itself:

```bash
curl -s https://api.v2.dizen.pro/v1/identity/health
```

The `commit` field is the one to compare against `git rev-parse main`.

If a newer commit reaches the branch while an older run waits for approval, the older run is
cancelled and its approval disappears. Approve the new one; deploying the superseded commit
is not what you want.

---

## Rollback

Every image is published as `sha-<short>` as well as the moving tag, and releases also carry
`v1.2.3`. Rolling back is naming one of those:

1. In the Dokploy project, set `IMAGE_TAG` to `sha-<short>` (or `v1.2.3`).
2. Deploy.

No pipeline, no rebuild, no waiting for a compile. Set `IMAGE_TAG` back to `production` once
the fix ships forward.

**Re-running an old pipeline run is not a rollback, and the pipeline refuses it.** The
`commit is current` job stops a run whose commit the branch has moved past, before anything
is built: its `images` job would otherwise republish the moving tag pointing at the old
commit, rewinding `:production` for every later deployment and not just that run.

**A migration that is not backwards compatible cannot be rolled back this way.** The rule is
that every migration must work against the previous version of the code; when it cannot, it
is split across two deployments. If an incompatible one has already been applied, rolling the
image back leaves the old code against the new schema — restore instead.

---

## Migrations

Each service applies its own at startup, under a Postgres session advisory lock so two
replicas cannot migrate at once. A failed migration keeps the container from becoming ready,
and the deploy stops there rather than half-applying.

Check what is applied:

```bash
make migrate-version SERVICE=tours
```

The DSN must be Neon's **direct** endpoint. The pooled one — the host with `-pooler` in it —
is PgBouncer in transaction mode, where the advisory lock is taken on one backend and
released on another, so it protects nothing, and it fails rarely and at the worst moment.

---

## Restore a backup

**Deliberately not armed** (D-45). The `backup` service exists behind the `backup` profile
in `deploy/docker-compose.prod.yml` and is enabled in neither environment: off-site copies
were deferred on cost, knowing what that leaves uncovered.

**So the recovery path today is Neon's point-in-time restore**, in the Neon console, within
its retention window. That covers the five databases, which is where all the real data is:
the production compose declares no volumes, RabbitMQ re-declares its exchange, queues and
bindings at startup, and Redis holds nothing any service reads yet. What it does not cover
is losing the Neon account itself.

Revisit when media storage lands -- an audio file an author uploaded is in no database --
or when tour content stops being something you could rebuild by hand.

Once enabled, the drill and the restore are:

```bash
make backup-drill                                   # dump, upload, restore, compare

deploy/backup/restore.sh                            # list what can be restored
deploy/backup/restore.sh identity "$DSN"            # show what would happen, write nothing
deploy/backup/restore.sh identity "$DSN" --yes      # do it
```

`restore.sh` writes nothing without `--yes`, because it drops and recreates every object in
the target and the one time it gets run is during an incident.

---

## Rotate a secret

All of them live in Doppler, one config per environment (`stg`, `prd`), read by Dokploy
through a read-only service token scoped to that config.

**JWT signing keys.** Generate both halves in one run — they are a pair, and mixing halves
from two runs means `identity` signs with a key nothing can verify:

```bash
make jwt-key
```

`JWT_PRIVATE_KEY_PEM` goes to `identity` alone. `JWT_PUBLIC_KEYS_PEM` goes to every service
and holds a list: during a rotation the retired public key stays there until the last token
it signed has expired, or every session in flight is dropped.

**Redis and RabbitMQ passwords.** Generate with `openssl rand -hex 32`, never `-base64`: the
value ends up inside a URL, and base64 produces `+`, `/` and `=`, which truncate the parse
and send credentials that are not the ones you set.

Changing the RabbitMQ password needs one more step — see the failure mode below.

**Dokploy API key.** Regenerate it in Dokploy, update the `DOKPLOY_API_KEY` repository
secret. Nothing else uses it.

---

## Reading an incident

Prometheus, Grafana, Jaeger and Loki are in the development compose and **not** in
production (RF-14 is open). Until they are, what is available:

```bash
curl -s https://api.v2.dizen.pro/v1/identity/health   # version, commit, clock
curl -s https://api.v2.dizen.pro/livez                # from inside only
```

`/livez`, `/readyz` and `/metrics` answer on the service port and are not published: only
`/v1/` is routed on the REST host. Reach them from the VPS, or read the container logs in
Dokploy.

On the VPS:

```bash
docker ps --format '{{.Names}}\t{{.Status}}' | grep dizen-v2
docker logs --tail 200 -f dizen-v2-production-identity-1
```

---

## Failure modes we have actually hit

**Traefik serves "TRAEFIK DEFAULT CERT".** Two causes, and the error names neither. Either
the DNS record does not resolve yet, so Let's Encrypt could not validate; or
`EXTRA_MIDDLEWARES` names a middleware Traefik never created — an empty `BASIC_AUTH_USERS`
creates no basicauth middleware, and Traefik silently **drops the entire router** that
references a missing one. In production both `EXTRA_MIDDLEWARES` and `BASIC_AUTH_USERS` are
empty.

**RabbitMQ: `user 'dizen' - invalid credentials`.** The broker applies
`RABBITMQ_DEFAULT_USER` and `RABBITMQ_DEFAULT_PASS` only on its **first** boot, with an empty
volume. Redeploying over an existing volume keeps the old password, silently. The message is
identical whether the user is missing or the password is wrong. Two ways out, and doing both
at once undoes the first:

```bash
docker exec dizen-v2-rabbitmq-<env> rabbitmqctl change_password dizen '<value>'
# or bump the volume name in deploy/rabbitmq/docker-compose.yml and redeploy clean
```

Nothing is lost by resetting the volume: the exchange, queues, dead-letter queues and
bindings are declared by the services at startup.

**The deployed version does not change.** `docker compose up` will not re-pull a moving tag
on its own; `pull_policy: always` is what makes it. If the version still lags, check whether
the pipeline reached its deploy job at all — a run that failed earlier publishes nothing.

**`manifest unknown` during a deploy.** Something deployed before the images existed. No
Dokploy trigger should be enabled: Autodeploy is off on every project, and the pipeline is
the only caller.

**gRPC client reports `invalid compression flag: 52`.** That is ASCII `4`, the first byte of
a plain-HTTP `404`. The request was not routed to a gRPC backend — check the host, not the
client. Each service has its own gRPC host (D-38); a shared one cannot route reflection,
because a reflection call does not say which service's schema it wants.

**A pipeline step ends with `Error: The operation was canceled.`** If a newer run exists on
the same branch, this one was superseded on purpose. It did not fail.

**`stream error ... INTERNAL_ERROR` downloading a module.** `proxy.golang.org` dropping a
transfer. The pipeline retries when the module cache missed; otherwise re-run the job.

**A second environment will not start on the VPS.** Redis and RabbitMQ bind a port on the
host's loopback, so two environments compete for it. `REDIS_HOST_PORT` and
`RABBITMQ_UI_PORT` differ per environment; the services themselves do not use these — they
connect by container name over `dokploy-network`.

---

## Deployment order across repositories

**The backend goes first, always** (RF-12b). Contract changes are additive, so a published
app keeps working against a newer backend; the reverse is not guaranteed. The backend's
release workflow does not depend on the other two.

---

## External dependencies

| What | Where | Holds |
|---|---|---|
| Postgres | Neon, one project per environment | the five databases |
| Secrets | Doppler, project `dizen-backend`, configs `stg` and `prd` | everything in `dokploy.env.example` |
| Host | Dokploy on the VPS | the compose projects, Traefik, Redis, RabbitMQ |
| Images | GHCR, `ghcr.io/nicodanke/dizen-v2-backend` | one repository per service |
| Certificates | Let's Encrypt through Traefik, HTTP-01 | renewed automatically |
| DNS | Hostinger | `*.v2.dizen.pro` |

The v1 stack still runs on the same VPS and owns `api.dizen.pro` and
`grpc-{user,booking,admin}.dizen.pro`. v2 lives under `*.v2.dizen.pro` so the two cannot
collide (D-31).
