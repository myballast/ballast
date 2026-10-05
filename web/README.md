# Ballast — web

Next.js App Router front end. See `docs/architecture.md` for the layout of
`src/` and the root `CLAUDE.md` for the rules it follows.

```
make web           # from the repository root, starts the dev server on :3000
npm run lint
npm run typecheck
```

The Go API must be running (`make api`). Its address is read from
`BALLAST_API_URL`, see `.env.example`.

The endpoints this front end calls are described in `docs/api/openapi.yaml`
(`make api-docs` from the repository root).
