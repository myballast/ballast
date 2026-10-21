-- Every statement here runs inside InTenantTx, so row-level security already
-- restricts it to one organization. The org_id and project_id in each WHERE are
-- for the plan and for the second project of the same organization, which row
-- level security says nothing about.

-- name: DeleteCatalogSource :execrows
DELETE FROM catalog_endpoint
WHERE org_id = $1 AND project_id = $2 AND source = $3;

-- Sent as a batch: a COPY is refused on a table with row-level security, and a
-- statement per endpoint in one round trip is fast enough for a few thousand.
-- name: CreateCatalogEndpoint :batchexec
INSERT INTO catalog_endpoint
    (id, org_id, project_id, source, method, path, tag, summary, path_params,
     headers, content_type, body, body_too_long, imported_by, created_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15);

-- name: CountCatalogEndpoints :one
SELECT count(*) FROM catalog_endpoint
WHERE org_id = $1 AND project_id = $2;

-- Tagged endpoints first, then by tag, path and method in the order a person
-- reads them. The query is looked for as a plain substring: position() takes no
-- pattern, so nothing somebody types can be read as one.
-- name: ListCatalogEndpoints :many
SELECT id, org_id, project_id, source, method, path, tag, summary, path_params,
       headers, content_type, body, body_too_long, imported_by, created_at
FROM catalog_endpoint
WHERE org_id = sqlc.arg(org_id) AND project_id = sqlc.arg(project_id)
  AND (sqlc.arg(tag)::text = '' OR tag = sqlc.arg(tag)::text)
  AND (sqlc.arg(source)::text = '' OR source = sqlc.arg(source)::text)
  AND (sqlc.arg(query)::text = ''
       OR position(lower(sqlc.arg(query)::text) in lower(method || ' ' || path || ' ' || summary || ' ' || tag)) > 0)
ORDER BY (tag = ''), tag, path,
         CASE method WHEN 'GET' THEN 0 WHEN 'POST' THEN 1 WHEN 'PUT' THEN 2
                     WHEN 'PATCH' THEN 3 WHEN 'DELETE' THEN 4 ELSE 5 END,
         id;

-- name: DeleteCatalogEndpoint :execrows
DELETE FROM catalog_endpoint
WHERE org_id = $1 AND project_id = $2 AND id = $3;
