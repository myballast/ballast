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

# The egress proxy is built from this repository rather than pulled: it is our
# own code, and a proxy from a registry could not re-resolve a host and refuse a
# private address. A machine that has not built it cannot run anything, which is
# the right failure — a run with no proxy would be a run with no way out.
PROXY_IMAGE := ballast/egress-proxy:dev

.PHONY: up
up: ## Start the local services and wait for them to be healthy
	docker compose up --detach --wait

.PHONY: down
down: stop ## Stop the API, the worker, the front-end and the local services
	# --remove-orphans matters here: a service that is removed from compose.yaml
	# stops being something Compose knows about, so its container is left running
	# and `make down` would never touch it again.
	docker compose down --remove-orphans

## --- Self-hosting ---------------------------------------------------------

# The whole application from images, for someone who runs Ballast rather than
# works on it: compose.selfhost.yaml. A project of its own, with its own
# database, so nothing here touches the development one.
#
# Run with an empty environment on purpose. This Makefile exports every value of
# the development .env to its recipes, and Compose prefers the shell's values to
# --env-file: without env -i, the development OAuth app and keys would quietly
# become the self-hosted instance's. Only what finds the Docker daemon is kept.
SELFHOST := env -i PATH="$$PATH" HOME="$$HOME" $${DOCKER_HOST:+DOCKER_HOST="$$DOCKER_HOST"} \
	$${DOCKER_CONTEXT:+DOCKER_CONTEXT="$$DOCKER_CONTEXT"} \
	docker compose -f compose.selfhost.yaml --env-file .env.selfhost

.PHONY: selfhost
selfhost: ## Build and start the whole application on http://localhost:3000
	sh deploy/selfhost/init-env.sh
	$(SELFHOST) --profile build build
	$(SELFHOST) up --detach
	@echo ""
	@echo "Ballast is starting on http://localhost:3000"
	@echo "  make selfhost-logs   follow what it does"
	@echo "  make selfhost-down   stop it (a run in progress is allowed to finish)"

.PHONY: selfhost-logs
selfhost-logs: ## Follow the logs of the self-hosted application
	$(SELFHOST) logs --follow

# Not `down -v`: the volume is the database. The worker drains first, so this
# waits for a run in progress to end, up to an hour and six minutes.
.PHONY: selfhost-down
selfhost-down: ## Stop the self-hosted application, keeping its data
	$(SELFHOST) down --remove-orphans

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

## --- Object storage -------------------------------------------------------

# Development defaults only, matching the minio service of compose.yaml: a value
# from the shell or from .env wins. These are the store's administrator, which is
# what creating a bucket takes; the application's own credentials cannot.
storage-check: export BALLAST_S3_ENDPOINT ?= 127.0.0.1:9000
storage-check: export BALLAST_S3_BUCKET ?= ballast
storage-check: export BALLAST_S3_ACCESS_KEY ?= ballast
storage-check: export BALLAST_S3_SECRET_KEY ?= ballastballast
storage-check: export BALLAST_S3_USE_SSL ?= false

.PHONY: storage-check
storage-check: ## Create the development bucket, then write, read and remove an object
	go run ./cmd/storagecheck -create-bucket

## --- Quality --------------------------------------------------------------

.PHONY: test
test: ## Run the unit tests
	go test ./...

# Export DATABASE_URL to target another database; the default DSN lives next
# to the tests that use it.
.PHONY: proxy-image
proxy-image: ## Build the egress proxy image, the only way out of a run's network
	docker build --quiet -f deploy/proxy/Dockerfile -t $(PROXY_IMAGE) .

.PHONY: test-int
test-int: proxy-image ## Run the integration tests, requires `make up`
	go test -tags=integration -count=1 ./...

.PHONY: lint
lint: ## Run golangci-lint
	$(GOLANGCI_LINT) run ./...

## --- API documentation ----------------------------------------------------

SWAGGER_UI_IMAGE := swaggerapi/swagger-ui:v5.17.14
REDOCLY_IMAGE := redocly/cli:1.34.5

.PHONY: api-docs
api-docs: ## Browse docs/api/openapi.yaml in Swagger UI on http://localhost:8081
	@echo "Swagger UI on http://localhost:8081 (Ctrl+C to stop)"
	docker run --rm -p 8081:8080 -e SWAGGER_JSON=/spec/openapi.yaml \
		-v "$(CURDIR)/docs/api:/spec:ro" $(SWAGGER_UI_IMAGE)

.PHONY: api-lint
api-lint: ## Validate docs/api/openapi.yaml against the OpenAPI specification
	docker run --rm -v "$(CURDIR)/docs/api:/spec:ro" -w /spec $(REDOCLY_IMAGE) lint openapi.yaml

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
#
# The database address has no default in the code on purpose: a process that
# falls back to localhost starts in production and fails on every request. The
# local address is right on this machine, so it is set here.
DEV_DATABASE_URL := postgres://ballast_app:ballast_app@127.0.0.1:5432/ballast?sslmode=disable
api: export BALLAST_DATABASE_URL ?= $(DEV_DATABASE_URL)
worker: export BALLAST_DATABASE_URL ?= $(DEV_DATABASE_URL)

api: export BALLAST_SESSION_SECRET ?= development-only-session-secret-do-not-deploy
api: export BALLAST_PUBLIC_URL ?= http://localhost:8080
api: export BALLAST_WEB_URL ?= http://localhost:3000

# Encrypts the secrets of an environment at rest. Both processes need it, and it
# has to be the same one on both, or the worker cannot open what the API wrote.
# This value is in a public repository: it is for this machine and nothing else.
# Generate a real one with `openssl rand -base64 32`.
DEV_SECRET_KEY := YmFsbGFzdC1kZXZlbG9wbWVudC1rZXktZG8tbm90LXU=
api: export BALLAST_SECRET_KEY ?= $(DEV_SECRET_KEY)
worker: export BALLAST_SECRET_KEY ?= $(DEV_SECRET_KEY)

# The organization deploy/seed/dev.sql creates, and the route that lets a
# generator reach a server on this machine. The development worker is
# restricted to it; with the variable empty, a worker serves every organization.
worker: export BALLAST_WORKER_ORG_ID ?= 417116f2-ad9b-4286-89cf-37930567de88
worker: export BALLAST_EXTRA_HOST ?= host.docker.internal:host-gateway
# A run launched here targets a server on this machine, whose address is private.
# The egress proxy refuses those unless the network is named, and this names the
# ranges a host gateway falls in. **Empty in production**: naming a private range
# lets a generator reach it.
worker: export BALLAST_EGRESS_ALLOWED_CIDRS ?= 10.0.0.0/8,172.16.0.0/12,192.168.0.0/16

# The seed is left out on purpose: it deletes the development organization, and
# every run with it. Run `make seed` once, by hand.
.PHONY: dev
dev: web/node_modules ## Start Postgres, migrate, then run the API, the worker and the front-end together
	$(MAKE) --no-print-directory up
	$(MAKE) --no-print-directory migrate
	$(MAKE) --no-print-directory proxy-image
	$(MAKE) --no-print-directory -j3 api worker web

.PHONY: api
api: ## Run the HTTP API locally
	$(call track,api)
	go run ./cmd/api

# The command line tool a pipeline runs. It is built on its own, with nothing of the
# server in it (a test keeps it so); bin/ is not tracked.
.PHONY: build-cli
build-cli: ## Build the ballast command line tool into bin/ballast
	go build -trimpath -o bin/ballast ./cmd/ballast

# A trial of what a release does, on this machine and without publishing anything: the
# workflow that publishes runs this same script. VERSION is the tag it would be released
# under, for example make release-cli VERSION=v0.1.0-rc.1. dist/ is not tracked.
.PHONY: release-cli
release-cli: ## Build the six binaries and checksums.txt of a release into dist/ (VERSION=vX.Y.Z)
	@test -n "$(VERSION)" || { echo "VERSION is required, for example: make release-cli VERSION=v0.1.0-rc.1"; exit 1; }
	bash ops/release/build.sh --check-version "$(VERSION)"
	rm -rf dist
	bash ops/release/build.sh "$(VERSION)" dist

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

.PHONY: web-test
web-test: web/node_modules ## Run the front-end's unit tests (Node's built-in runner)
	npm --prefix web run test
