# Ballast — development entry points.
# Targets that have no implementation yet fail loudly rather than lying.

MIGRATE_VERSION := v4.18.3
MIGRATE := go run -tags postgres github.com/golang-migrate/migrate/v4/cmd/migrate@$(MIGRATE_VERSION)

# Migrations connect as the owner role. ballast_app has no DDL rights, and that
# is the point: it must not be able to drop a policy. The bookkeeping table is
# kept out of the public schema, which holds tenant-scoped tables only.
MIGRATE_DATABASE_URL ?= postgres://ballast:ballast@127.0.0.1:5432/ballast?sslmode=disable&x-migrations-table="migrate"."schema_migrations"&x-migrations-table-quoted=1

GOLANGCI_LINT_VERSION := v2.1.6
GOLANGCI_LINT := $(shell command -v golangci-lint 2>/dev/null)
ifeq ($(GOLANGCI_LINT),)
GOLANGCI_LINT := go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)
endif

# $(call todo,<ticket>) — placeholder for a target owned by a later ticket.
define todo
	@echo "$@: not implemented yet ($(1))"
	@exit 1
endef

.DEFAULT_GOAL := help

.PHONY: help
help: ## List the available targets
	@grep -hE '^[a-z-]+:.*?## ' $(MAKEFILE_LIST) | awk -F':.*?## ' '{printf "  %-14s %s\n", $$1, $$2}'

## --- Local dependencies ---------------------------------------------------

.PHONY: up
up: ## Start PostgreSQL and MinIO, and wait for them to be healthy
	docker compose up --detach --wait

.PHONY: down
down: ## Stop PostgreSQL and MinIO
	docker compose down

## --- Database -------------------------------------------------------------

.PHONY: migrate
migrate: ## Apply the database migrations
	$(MIGRATE) -path migrations -database '$(MIGRATE_DATABASE_URL)' up

.PHONY: migrate-down
migrate-down: ## Roll every migration back
	$(MIGRATE) -path migrations -database '$(MIGRATE_DATABASE_URL)' down -all

.PHONY: sqlc
sqlc: ## Regenerate the Go code from the SQL queries
	$(call todo,T03)

## --- Quality --------------------------------------------------------------

.PHONY: test
test: ## Run the unit tests
	go test ./...

# Export DATABASE_URL to target another database; the default DSN lives next
# to the tests that use it.
.PHONY: test-int
test-int: ## Run the integration tests, requires `make up`
	go test -tags=integration -count=1 ./...

.PHONY: lint
lint: ## Run golangci-lint
	$(GOLANGCI_LINT) run ./...

## --- Processes ------------------------------------------------------------

.PHONY: api
api: ## Run the HTTP API locally
	$(call todo,T11)

.PHONY: worker
worker: ## Run the worker locally
	$(call todo,T11)

.PHONY: web
web: ## Run the front-end locally
	$(call todo,T12)
