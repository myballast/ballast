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
down: stop ## Stop the API, the worker, the front-end and the local services
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

# Long-lived processes record their process group here, so that `make stop`
# can reach them from another terminal.
RUN_DIR := .run

# $(call track,<name>) records the process group of the make running <name>.
# Under `make dev` the three processes share one group, which is the point:
# signalling it is the same as pressing Ctrl+C in that terminal.
define track
	@mkdir -p $(RUN_DIR)
	@ps -o pgid= -p $$$$ | tr -d ' ' > $(RUN_DIR)/$(1).pgid
endef

# stop interrupts every recorded group, then waits for it to be gone: the
# worker must be able to finish cleaning up while Postgres is still there.
# A group is signalled only if it still runs one of our processes, since a
# number left behind by a crash may since have gone to something else. The
# [n] keeps the pattern from matching the grep that carries it.
.PHONY: stop
stop: ## Stop the API, the worker and the front-end, wherever they were started
	@self=$$(ps -o pgid= -p $$$$ | tr -d ' '); \
	for pg in $$(cat $(RUN_DIR)/*.pgid 2>/dev/null | sort -u); do \
		[ "$$pg" = "$$self" ] && continue; \
		if ps -eo pgid=,command= | awk -v pg="$$pg" '$$1 == pg' | grep -qE 'cmd/(api|worker)|[n]ext dev'; then \
			echo "stopping process group $$pg"; \
			kill -INT -"$$pg" 2>/dev/null || true; \
			for _ in $$(seq 1 30); do \
				ps -eo pgid= | grep -qx " *$$pg" || break; \
				sleep 0.5; \
			done; \
		fi; \
	done
	@rm -rf $(RUN_DIR)

# Local settings and secrets, such as the OAuth app credentials, live in a
# .env file that git ignores; .env.example lists them. Every variable it sets
# is exported to the processes below.
ifneq (,$(wildcard .env))
include .env
export $(shell sed -n 's/^\([A-Z_][A-Z0-9_]*\)=.*/\1/p' .env)
endif

# Development defaults only: a value from the shell or from .env wins. The
# session secret below signs cookies on this machine and nowhere else.
api: export BALLAST_SESSION_SECRET ?= development-only-session-secret-do-not-deploy
api: export BALLAST_PUBLIC_URL ?= http://localhost:8080
api: export BALLAST_WEB_URL ?= http://localhost:3000

# The organization deploy/seed/dev.sql creates, and the route that lets a
# generator reach a server on this machine. The worker serves one organization
# until the queue can serve them all.
worker: export BALLAST_WORKER_ORG_ID ?= 00000000-0000-4000-8000-000000000001
worker: export BALLAST_EXTRA_HOST ?= host.docker.internal:host-gateway

# The seed is left out on purpose: it deletes the development organization, and
# every run with it. Run `make seed` once, by hand.
.PHONY: dev
dev: web/node_modules ## Start Postgres, migrate, then run the API, the worker and the front-end together
	$(MAKE) --no-print-directory up
	$(MAKE) --no-print-directory migrate
	$(MAKE) --no-print-directory -j3 api worker web

.PHONY: api
api: ## Run the HTTP API locally
	$(call track,api)
	go run ./cmd/api

.PHONY: worker
worker: ## Run the worker locally
	$(call track,worker)
	go run ./cmd/worker

.PHONY: web
web: web/node_modules ## Run the front-end locally
	$(call track,web)
	npm --prefix web run dev

web/node_modules: web/package-lock.json
	npm --prefix web ci
	@touch $@

.PHONY: web-lint
web-lint: web/node_modules ## Lint and type-check the front-end
	npm --prefix web run lint
	npm --prefix web run typecheck
