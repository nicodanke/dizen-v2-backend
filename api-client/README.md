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

Nothing is marked *sharable* in Yaak, which is what keeps a value entered in the app from
being synced back into git.

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
