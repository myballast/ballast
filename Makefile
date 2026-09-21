# Ballast — development entry points.
# Targets that have no implementation yet fail loudly rather than lying.

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
	@grep -hE '^[a-z-]+:.*?## ' $(MAKEFILE_LIST) | awk -F':.*?## ' '{printf "  %-10s %s\n", $$1, $$2}'

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
	$(call todo,T02)

.PHONY: sqlc
sqlc: ## Regenerate the Go code from the SQL queries
	$(call todo,T02)

## --- Quality --------------------------------------------------------------

.PHONY: test
test: ## Run the unit tests
	go test ./...

.PHONY: test-int
test-int: ## Run the integration tests, requires `make up`
	$(call todo,T03)

.PHONY: lint
lint: ## Run golangci-lint
	$(GOLANGCI_LINT) run ./...

## --- Processes ------------------------------------------------------------

.PHONY: api
api: ## Run the HTTP API locally
	$(call todo,T04)

.PHONY: worker
worker: ## Run the worker locally
	$(call todo,T04)

.PHONY: web
web: ## Run the front-end locally
	$(call todo,T25)
