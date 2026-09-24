# Ballast — development entry points.
# Targets that have no implementation yet fail loudly rather than lying.

MIGRATE_VERSION := v4.18.3
MIGRATE := go run -tags postgres github.com/golang-migrate/migrate/v4/cmd/migrate@$(MIGRATE_VERSION)

# Migrations connect as the owner role. ballast_app has no DDL rights, and that
# is the point: it must not be able to drop a policy. The bookkeeping table is
# kept out of the public schema, which holds tenant-scoped tables only.
MIGRATE_DATABASE_URL ?= postgres://ballast:ballast@127.0.0.1:5432/ballast?sslmode=disable&x-migrations-table="migrate"."schema_migrations"&x-migrations-table-quoted=1

SQLC_VERSION := 1.27.0
SQLC := $(shell command -v sqlc 2>/dev/null)
ifeq ($(SQLC),)
# sqlc does not build from source on every host: its PostgreSQL parser is cgo.
# The official image sidesteps that, and Docker is already required to run k6.
SQLC := docker run --rm -v "$(CURDIR):/src" -w /src --user "$(shell id -u):$(shell id -g)" sqlc/sqlc:$(SQLC_VERSION)
endif

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
up: ## Start the local services and wait for them to be healthy
	docker compose up --detach --wait

.PHONY: down
down: ## Stop the local services
	# --remove-orphans matters here: a service that is removed from compose.yaml
	# stops being something Compose knows about, so its container is left running
	# and `make down` would never touch it again.
	docker compose down --remove-orphans

## --- Database -------------------------------------------------------------

.PHONY: migrate
migrate: ## Apply the database migrations
	$(MIGRATE) -path migrations -database '$(MIGRATE_DATABASE_URL)' up

.PHONY: migrate-down
migrate-down: ## Roll every migration back
	$(MIGRATE) -path migrations -database '$(MIGRATE_DATABASE_URL)' down -all

.PHONY: sqlc
sqlc: ## Regenerate the Go code from the SQL queries
	$(SQLC) generate

.PHONY: seed
seed: ## Load the development organization, project, scenario and environment
	docker compose exec -T -e PGOPTIONS=--client-min-messages=warning postgres \
		psql -q -v ON_ERROR_STOP=1 -U ballast -d ballast < deploy/seed/dev.sql

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
	go run ./cmd/api

.PHONY: worker
worker: ## Run the worker locally
	go run ./cmd/worker

.PHONY: web
web: ## Run the front-end locally
	$(call todo,T12)
