# api-client

Versioned [Yaak](https://yaak.app/docs) collection for the Dizen API (RF-17c, `03` section 8).

**A new endpoint is not finished until it has its request here**, the same way it is not
finished without a test.

## Setup

1. Open Yaak and create or open the `Dizen` workspace.
2. Workspace Settings, then sync with a local directory, and point it at this folder.
3. Pick the `Local` environment. **It comes filled in**, so after `make up` the requests work
   with nothing to type.

For `Staging` and `Production`, fill in the endpoints yourself: those hostnames are not
committed.

## What is committed and what is not

The line is not "values yes or no", it is what is safe to commit:

| | Committed | Why |
|---|---|---|
| `Local` endpoints | **yes** | They are the ports `deploy/docker-compose.yml` publishes: constants of the development environment, the same on every machine, already written down in the README |
| `Staging` and `Production` endpoints | no | Not derivable from the repository |
| `access_token`, `refresh_token` | **never**, in any environment | Filled in at run time by the sign-in request |

## Only `Local` is public in Yaak

Yaak excludes a non-public environment from Directory Sync entirely: it does not write the
file, and it deletes the one it finds in the directory. Environments are the only model with
that flag -- folders and requests are always synced -- which is why they were the only files
that kept disappearing from this folder.

**`Local` is marked public and the other three are not**, and that is the arrangement, not an
accident (D-29):

- `Local` has to be versioned for the collection to work right after `make up`, and it is
  safe to version because the check below lets it hold nothing but a localhost endpoint or a
  semver.
- `Staging` and `Production` are exactly where somebody types a real token. Leaving them out
  of the sync keeps them out of git **by construction**, which is stronger than a check
  catching them on the way in.
- `Base` only carries variable names, so versioning it buys little.

`make api-client` therefore requires `Local` and accepts the absence of the other three --
but validates any of them that does appear, so marking one public later cannot smuggle a
value past it.

What keeps a secret out of git is the check:

- **`make api-client`** rejects a value in any credential variable (`access_token`,
  `refresh_token`, `password`, `api_key`, ...) in *any* environment, any value at all outside
  `Local`, anything inside `Local` that is not a `localhost` endpoint or a semver, and
  anything shaped like a credential even if the variable is not on the list.
- **`gitleaks`** runs over the working tree and the whole history in CI.

Both are blocking. Type a token into `Local` in the app and Yaak will write it to disk, and
the commit will be refused.

## These files belong to Yaak

The name of each file is the resource `id`, which is what Directory Sync writes:
`yaak.env_local.yaml`, `yaak.fl_tours.yaml`, `yaak.hr_identity_livez.yaml`. A file named
anything else is one the app does not recognize as its own.

For the same reason, **the explanation lives here and not in the files**: Yaak rewrites them
on every sync and any comment inside them is lost.

## Rules

- One folder per service; requests named after the RPC.
- Flow requests leave the token in an environment variable: `SignIn` stores `access_token`
  and everything else reads it. Nobody pastes a JWT by hand.
- `make api-client` validates that every file parses, that the collection is complete (the
  workspace, all four environments and at least one request), and that no credential is
  committed. It refuses a token in any environment, any value outside `Local`, and anything
  in `Local` that is not a localhost endpoint or a version.

## Relationship with `gen/openapi/`

They do not compete. `gen/openapi/` is the generated contract: what the API accepts. This
collection is how it is actually used: the payload that works, the order of calls in a flow,
the edge case that broke something once. That does not come out of a generator.
