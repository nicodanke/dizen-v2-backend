# Pinned versions of the build tools.
#
# They are installed with `go install <pkg>@<version>` into ./bin rather than declared as
# `tool` dependencies of the root module: buf, golangci-lint and sqlc share transitive
# dependencies that conflict when they live in the same module graph. This file is the
# single source of truth for versions, consumed by the Makefile and by CI.

BUF_VERSION                    := v1.72.0
PROTOC_GEN_GO_VERSION          := v1.36.12
PROTOC_GEN_GO_GRPC_VERSION     := v1.6.2
PROTOC_GEN_GRPC_GATEWAY_VERSION:= v2.30.0
PROTOC_GEN_OPENAPI_VERSION     := v0.7.1
SQLC_VERSION                   := v1.31.1
MOCKERY_VERSION                := v3.7.4
MIGRATE_VERSION                := v4.19.1
GOLANGCI_LINT_VERSION          := v2.13.2

# protoc-gen-dart cannot be installed with `go install`: it ships with the Dart SDK.
PROTOC_PLUGIN_VERSION          := 25.0.0
