# Ballast

Load testing as a service, for several projects at once. A user picks a project,
a k6 scenario and a target environment, queues a run, and reads the results.
Load is only ever sent to environments whose ownership has been verified.

- `docs/cahier-des-charges.md` — scope, requirements, acceptance criteria
- `docs/architecture.md` — repository layout and layer rules
- `docs/api/openapi.yaml` — the HTTP API's contract (see [API](#api))
- `CLAUDE.md` — the rules every change follows

## Prerequisites

- Go 1.23
- Docker (Postgres, the k6 generators, and the API documentation tools)
- Node.js 22 and npm (the front end)

## Getting started

```bash
cp .env.example .env      # then fill in the GitHub OAuth app, see below
make up && make migrate
make seed                 # once: a development organization, scenario and environment
make dev                  # API on :8080, worker, front end on :3000
```

`make down` stops everything, wherever it was started.

### Signing in

Ballast stores no password: people sign in with GitHub or Google. For local
development, create a GitHub OAuth app at https://github.com/settings/developers:

- Homepage URL: `http://localhost:3000`
- Authorization callback URL: `http://localhost:8080/auth/github/callback`

and put its client ID and secret in `.env`. Never in `.env.example`, which is
committed.

The first sign-in creates an organization, with a first project its user
administers. `make worker` only takes runs from `BALLAST_WORKER_ORG_ID` (the
seeded organization by default); a worker with that variable empty serves every organization.

## Commands

| Command | |
|---|---|
| `make dev` | Postgres, migrations, then the API, the worker and the front end together |
| `make api` / `make worker` / `make web` | One process at a time |
| `make stop` / `make down` | Stop the processes / and Postgres |
| `make migrate` | Apply the database migrations |
| `make sqlc` | Regenerate the Go code from `internal/adapter/postgres/queries` |
| `make test` | Unit tests, including the API contract check |
| `make test-int` | Integration tests, needs `make up` and Docker |
| `make lint` / `make web-lint` | Go / front-end linters |
| `make api-docs` | Swagger UI on http://localhost:8081 |
| `make api-lint` | Validate the OpenAPI spec |

## API

The contract is `docs/api/openapi.yaml`, OpenAPI 3.1, written by hand.

```bash
make api-docs   # browse it in Swagger UI on http://localhost:8081
make api-lint   # validate it
```

Every route except the health check and the sign-in flow needs the
`ballast_session` cookie, which signing in sets. It is `HttpOnly`, so Swagger
UI's "Try it out" cannot send it; use the front end, or `curl` with the cookie
copied from the browser's developer tools.

**Keeping it true.** Any change that adds, modifies or removes an endpoint
updates `openapi.yaml` in the same change. `make test` enforces the part a
machine can check: `internal/adapter/http/openapi_test.go` fails when a route
registered in `internal/adapter/http/router.go` is missing from the spec, when
a documented route no longer exists, or when a route is public on one side and
behind a session on the other. Request and response shapes come from
`internal/adapter/http/dto/`.
