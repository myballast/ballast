# Contributing to Ballast

Thank you for wanting to help. Ballast is source-available under the
[Functional Source License](LICENSE) (FSL-1.1-ALv2).

## Before you start

- For anything bigger than a small fix, **open an issue first** and say what you
  want to change, so that nobody spends a week on something that cannot be merged.
- **Security problems are not issues.** See [SECURITY.md](SECURITY.md).

## Setting up

See the *Getting started* section of the [README](README.md). In short:

```bash
cp .env.example .env
make up && make migrate
make seed
make dev
```

## Making a change

1. Fork the repository and create a branch from `main`.
2. Keep the change small and focused. One concern per pull request.
3. Write the test first for anything that touches isolation between
   organizations, the verification of targets, or the handling of secrets.
4. Run the checks and make sure they pass:

```bash
make test && make lint
cd web && npm run lint && npm run typecheck && npm test
```

5. If you add, change or remove an API endpoint, update `docs/api/openapi.yaml`
   in the same change, and run `make api-lint`. `make test` fails when the
   router and the spec disagree.
6. Open a pull request and describe what changed and why.

## Rules that are never relaxed

These protect users from each other and from being attacked through Ballast.
A pull request that weakens one of them will not be merged:

- every table carries `org_id`, has Row-Level Security, and is only reached
  through `InTenantTx`;
- no run is queued against an environment whose ownership is not verified;
- the load generator has no route to the network except the filtering proxy;
- a secret is never returned by the API, never written in a script or a log.

## Code style

- Go: `make lint` (golangci-lint) must pass. Interfaces are declared by the code
  that uses them; `internal/domain` imports nothing from the project.
- Front end: server components by default; the logic lives in
  `web/src/features/<domain>/`. `npm run lint` also checks the colour charter.
- Code, identifiers, comments and commit messages are in English.

## Sign-off (DCO)

Every commit must carry a `Signed-off-by` line, which certifies that you wrote
the change or have the right to submit it under this project's license (the
[Developer Certificate of Origin](https://developercertificate.org/)). Add it
with:

```bash
git commit -s -m "your message"
```

By contributing, you also agree that your contribution may be distributed under
the project's license, and under a different license the maintainer may choose
for the project in the future.
