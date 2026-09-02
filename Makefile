# Makefile for dizen-v2-backend.
#
# Every day-to-day command goes through here. Tools are installed pinned into ./bin (see
# tools/versions.mk) so that no build depends on what each machine happens to have
# installed.

include tools/versions.mk

SHELL := /usr/bin/env bash
.DEFAULT_GOAL := help

ROOT_DIR   := $(CURDIR)
TOOLS_BIN  := $(ROOT_DIR)/bin
FOR_EACH   := $(ROOT_DIR)/scripts/for-each-module.sh

export PATH := $(TOOLS_BIN):$(PATH)

BUF            := $(TOOLS_BIN)/buf
SQLC           := $(TOOLS_BIN)/sqlc
MOCKERY        := $(TOOLS_BIN)/mockery
MIGRATE        := $(TOOLS_BIN)/migrate
GOLANGCI_LINT  := $(TOOLS_BIN)/golangci-lint
GITLEAKS       := $(TOOLS_BIN)/gitleaks

SERVICES := identity tours booking admin mail-dispatcher

.PHONY: help
help: ## Show this help
	@grep -hE '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) \
		| sort \
		| awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-22s\033[0m %s\n", $$1, $$2}'

# ---------------------------------------------------------------------------
# Tools
# ---------------------------------------------------------------------------

.PHONY: doctor
doctor: ## Check that everything needed to work on the repository is installed and running
	@$(ROOT_DIR)/scripts/doctor.sh

.PHONY: tools
tools: ## Install the tools pinned in tools/versions.mk into ./bin
	@mkdir -p $(TOOLS_BIN)
	@echo "==> installing tools into $(TOOLS_BIN)"
	@GOWORK=off GOBIN=$(TOOLS_BIN) go install github.com/bufbuild/buf/cmd/buf@$(BUF_VERSION)
	@GOWORK=off GOBIN=$(TOOLS_BIN) go install google.golang.org/protobuf/cmd/protoc-gen-go@$(PROTOC_GEN_GO_VERSION)
	@GOWORK=off GOBIN=$(TOOLS_BIN) go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@$(PROTOC_GEN_GO_GRPC_VERSION)
	@GOWORK=off GOBIN=$(TOOLS_BIN) go install github.com/grpc-ecosystem/grpc-gateway/v2/protoc-gen-grpc-gateway@$(PROTOC_GEN_GRPC_GATEWAY_VERSION)
	@GOWORK=off GOBIN=$(TOOLS_BIN) go install github.com/google/gnostic/cmd/protoc-gen-openapi@$(PROTOC_GEN_OPENAPI_VERSION)
	@GOWORK=off GOBIN=$(TOOLS_BIN) go install github.com/sqlc-dev/sqlc/cmd/sqlc@$(SQLC_VERSION)
	@GOWORK=off GOBIN=$(TOOLS_BIN) go install github.com/vektra/mockery/v3@$(MOCKERY_VERSION)
	@GOWORK=off GOBIN=$(TOOLS_BIN) go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@$(MIGRATE_VERSION)
	@GOWORK=off GOBIN=$(TOOLS_BIN) go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)
	@GOWORK=off GOBIN=$(TOOLS_BIN) go install github.com/zricethezav/gitleaks/v8@$(GITLEAKS_VERSION)
	@# Dart is optional: it is only needed to regenerate the Dart package, and the generated
	@# code is committed. Missing it must not block a first run, so this warns instead of
	@# failing. `make tools-dart` on its own is strict, because asking for it explicitly
	@# means you need it.
	@if command -v dart >/dev/null 2>&1; then \
		$(MAKE) --no-print-directory tools-dart; \
	else \
		echo ""; \
		echo "note: the Dart SDK is not installed, so protoc-gen-dart was skipped."; \
		echo "      Everything works without it except 'make proto'."; \
		echo "      Install it with: brew install dart-sdk && make tools-dart"; \
		echo ""; \
	fi
	@echo "==> done"

.PHONY: tools-dart
tools-dart: ## Install protoc-gen-dart (requires the Dart SDK)
	@if ! command -v dart >/dev/null 2>&1; then \
		echo "error: the Dart SDK is missing; it is required to generate gen/dart/dizen_api" >&2; \
		echo "       install it with: brew install dart-sdk" >&2; \
		exit 1; \
	fi
	@dart pub global activate protoc_plugin $(PROTOC_PLUGIN_VERSION)
	@mkdir -p $(TOOLS_BIN)
	@# buf looks for plugins on the PATH; symlink it into ./bin so we do not depend on
	@# ~/.pub-cache/bin being configured on every machine.
	@ln -sf "$$HOME/.pub-cache/bin/protoc-gen-dart" $(TOOLS_BIN)/protoc-gen-dart

# ---------------------------------------------------------------------------
# Build and quality
# ---------------------------------------------------------------------------

.PHONY: build
build: ## Build every module in the workspace (binaries go to dist/)
	@ROOT_DIR=$(ROOT_DIR) $(FOR_EACH) $(ROOT_DIR)/scripts/build.sh

.PHONY: vet
vet: ## Run go vet across every module
	@$(FOR_EACH) go vet ./...

.PHONY: fmt
fmt: ## Format the code (gofumpt + goimports)
	@$(GOLANGCI_LINT) fmt

.PHONY: fmt-check
fmt-check: ## Check formatting without modifying files
	@$(GOLANGCI_LINT) fmt --diff

# embedlit is disabled: it rewrites a nested composite literal into promoted fields, which
# only compiles from Go 1.27. The toolchain here is 1.27, but every editor whose gopls is
# older reports the result as an error, so the whole team would see false failures for a
# rewrite that buys nothing. The same analyzer is disabled in .golangci.yml.
GO_FIX_FLAGS := -embedlit=false

.PHONY: fix
fix: ## Apply the modernizations suggested by `go fix` (Go 1.27+)
	@$(FOR_EACH) go fix $(GO_FIX_FLAGS) ./...
	@$(MAKE) --no-print-directory fmt

.PHONY: fix-check
fix-check: ## Show pending modernizations without applying them
	@$(FOR_EACH) go fix $(GO_FIX_FLAGS) -diff ./...

.PHONY: lint
lint: ## Run golangci-lint across every module
	@$(FOR_EACH) $(GOLANGCI_LINT) run --config $(ROOT_DIR)/.golangci.yml

# RANGE overrides what is checked; the default is the commits of this branch against main.
.PHONY: commit-check
commit-check: ## Check the commit messages of this branch (Conventional Commits, PRD-25 RF-14)
	@go run ./tools/commitcheck $(RANGE)

.PHONY: secrets-scan
secrets-scan: ## Scan the working tree and the history for committed secrets (RNF-4)
	@$(ROOT_DIR)/scripts/secrets-scan.sh

# `go mod tidy` deliberately ignores go.work: it resolves each module on its own, so on a
# service module it tries to download .../pkg from GitHub, which is never published (01
# section 3 forbids a per-service replace, and that is the price). The workspace-aware
# operation is `go work sync`, which propagates the resolved build list into every module.
#
# So: tidy the two modules that can be tidied -- the root and pkg have no intra-repository
# dependency -- and sync the rest.
.PHONY: tidy
tidy: ## Tidy the root and pkg, and sync the workspace into every module
	@printf '\033[1;34m==> root\033[0m\n'
	@go mod tidy
	@printf '\033[1;34m==> pkg\033[0m\n'
	@cd pkg && go mod tidy
	@printf '\033[1;34m==> go work sync\033[0m\n'
	@go work sync

.PHONY: tidy-check
tidy-check: ## Fail if `make tidy` leaves an uncommitted diff
	@$(ROOT_DIR)/scripts/tidy-check.sh

.PHONY: test
test: ## Unit tests across every module (fast, no Docker)
	@$(FOR_EACH) go test -race ./...

.PHONY: test-integration
test-integration: ## Integration tests with testcontainers (requires Docker)
	@$(FOR_EACH) go test -tags=integration -count=1 -timeout=30m ./...

.PHONY: test-images
test-images: ## Print the container images the integration tests use
	@$(ROOT_DIR)/scripts/test-images.sh

.PHONY: test-coverage
test-coverage: ## Full coverage with the 70% gate (RF-18b)
	@$(ROOT_DIR)/scripts/coverage.sh

.PHONY: coverage-html
coverage-html: ## Open the coverage report in a browser
	@go tool cover -html=$(ROOT_DIR)/coverage.filtered.out

# ---------------------------------------------------------------------------
# Contracts (protos)
# ---------------------------------------------------------------------------

.PHONY: proto
proto: proto-lint ## Regenerate Go, gateway, OpenAPI and the Dart package
	@echo "==> Go + grpc + gateway + Dart"
	@cd proto && $(BUF) generate
	@echo "==> OpenAPI v3"
	@$(ROOT_DIR)/scripts/gen-openapi.sh
	@echo "==> Dart package"
	@$(ROOT_DIR)/scripts/proto-postgen.sh
	@echo "==> done"

.PHONY: proto-lint
proto-lint: ## Lint the protos
	@cd proto && $(BUF) lint

.PHONY: proto-format
proto-format: ## Format the protos
	@cd proto && $(BUF) format -w

.PHONY: proto-breaking
proto-breaking: ## Check contract compatibility against main
	@cd proto && $(BUF) breaking --against '../.git#branch=main,subdir=proto'

.PHONY: proto-check
proto-check: ## Fail if `make proto` leaves an uncommitted diff (hard rule 3)
	@$(ROOT_DIR)/scripts/proto-check.sh

# ---------------------------------------------------------------------------
# Local environment
# ---------------------------------------------------------------------------

COMPOSE := docker compose -f $(ROOT_DIR)/deploy/docker-compose.yml

.PHONY: up
up: ## Bring up the whole local environment (secrets from Doppler when configured)
	@$(ROOT_DIR)/scripts/local-up.sh

.PHONY: down
down: ## Tear down the local environment, keeping the volumes
	@$(COMPOSE) down

.PHONY: down-clean
down-clean: ## Tear down the local environment and delete the data
	@$(COMPOSE) down -v

.PHONY: logs
logs: ## Follow the logs. Usage: make logs [SERVICE=identity]
	@$(COMPOSE) logs -f $(SERVICE)

.PHONY: ps
ps: ## Show the state of the environment
	@$(COMPOSE) ps

.PHONY: restart
restart: ## Restart one service. Usage: make restart SERVICE=identity
	@$(COMPOSE) restart $(SERVICE)

COMPOSE_PROD := docker compose -f $(ROOT_DIR)/deploy/docker-compose.prod.yml \
	--env-file $(ROOT_DIR)/deploy/dokploy.env.example

.PHONY: deploy-check
deploy-check: ## Validate the production compose against the documented variables (RF-3)
	@$(ROOT_DIR)/scripts/deploy-check.sh

.PHONY: backup-drill
backup-drill: ## Restore drill: dump, upload, restore and compare against real containers (RF-9)
	@$(ROOT_DIR)/scripts/backup-drill.sh

.PHONY: seed
seed: ## Load sample data into the local environment
	@$(ROOT_DIR)/scripts/seed.sh

# ---------------------------------------------------------------------------
# Database
# ---------------------------------------------------------------------------

.PHONY: jwt-key
jwt-key: ## Generate an Ed25519 key pair for a local .env
	@$(ROOT_DIR)/scripts/jwt-key.sh

.PHONY: jwt-key-pem
jwt-key-pem: ## Generate an Ed25519 key pair as real PEM, for Doppler
	@$(ROOT_DIR)/scripts/jwt-key.sh -pem

.PHONY: sqlc
sqlc: ## Regenerate the typed queries for every service
	@for svc in $(SERVICES); do \
		printf '\033[1;34m==> %s\033[0m\n' "$$svc"; \
		$(SQLC) generate -f services/$$svc/sqlc.yaml || exit 1; \
	done

.PHONY: sqlc-check
sqlc-check: ## Fail if `make sqlc` leaves an uncommitted diff (hard rule 3)
	@$(ROOT_DIR)/scripts/sqlc-check.sh

.PHONY: sqlc-vet
sqlc-vet: ## Run sqlc's own checks over the queries
	@for svc in $(SERVICES); do \
		printf '\033[1;34m==> %s\033[0m\n' "$$svc"; \
		$(SQLC) vet -f services/$$svc/sqlc.yaml || exit 1; \
	done

.PHONY: migrate-up
migrate-up: ## Apply pending migrations. Usage: make migrate-up SERVICE=tours
	@$(ROOT_DIR)/scripts/migrate.sh up $(SERVICE)

.PHONY: migrate-down
migrate-down: ## Roll back one migration. Usage: make migrate-down SERVICE=tours
	@$(ROOT_DIR)/scripts/migrate.sh down $(SERVICE) 1

.PHONY: migrate-create
migrate-create: ## Create a migration. Usage: make migrate-create SERVICE=tours NAME=add_x
	@$(ROOT_DIR)/scripts/migrate.sh create $(SERVICE) $(NAME)

.PHONY: migrate-version
migrate-version: ## Show the applied version. Usage: make migrate-version SERVICE=tours
	@$(ROOT_DIR)/scripts/migrate.sh version $(SERVICE)

# ---------------------------------------------------------------------------
# API collection (Yaak)
# ---------------------------------------------------------------------------

.PHONY: grpc-check
grpc-check: ## Verify gRPC over TLS against a deployed host. Usage: make grpc-check HOST=grpc.staging.v2.dizen.pro:443
	@$(ROOT_DIR)/scripts/grpc-check.sh $(HOST)

.PHONY: api-client
api-client: ## Validate the versioned Yaak collection (RF-17c)
	@$(ROOT_DIR)/scripts/api-client-check.sh

# ---------------------------------------------------------------------------
# Mocks
# ---------------------------------------------------------------------------

.PHONY: mocks
mocks: ## Regenerate the mocks over the repository interfaces
	@$(MOCKERY)

# ---------------------------------------------------------------------------
# Housekeeping
# ---------------------------------------------------------------------------

.PHONY: clean
clean: ## Remove tool binaries and coverage artifacts
	@rm -rf $(TOOLS_BIN) dist coverage.out coverage.html coverage.filtered.out
