# dizen_api

Generated Dart client for the Dizen gRPC contract.

**This package is generated code.** It is produced by `make proto` from `proto/` in the
`dizen-v2-backend` repository, which owns the contract. Do not edit it by hand: any change
is lost on the next generation.

## How it is consumed

From `dizen-v2-mobile`, pinned to a contract tag:

```yaml
dependencies:
  dizen_api:
    git:
      url: https://github.com/nicodanke/dizen-v2-backend.git
      path: gen/dart/dizen_api
      ref: api-v0.1.0
```

The `api-vX.Y.Z` tag is created by the backend CI whenever the contract changes, and it
opens the bump pull request in `dizen-v2-mobile` (01 sections 3.1 and 3.2).
