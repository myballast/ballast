# Ballast — web

Next.js App Router front end. `app/` holds routing and composition only; the
logic lives in `src/features/<domain>/`.

```
make web           # from the repository root, starts the dev server on :3000
npm run lint
npm run typecheck   # next typegen, then tsc
npm test
```

The Go API must be running (`make api`). Its address is read from
`BALLAST_API_URL`, see `.env.example`.

The endpoints this front end calls are described in `docs/api/openapi.yaml`
(`make api-docs` from the repository root).
