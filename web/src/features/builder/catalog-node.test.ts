import assert from "node:assert/strict";
import { test } from "node:test";

import type { CatalogEndpoint } from "../endpoints/types.ts";
import { fromEndpoint } from "./catalog-node.ts";
import { hasProblems, requestProblems } from "./request-validation.ts";

const endpoint = (over: Partial<CatalogEndpoint>): CatalogEndpoint => ({
  id: "e1",
  source: "openapi",
  method: "POST",
  path: "/users/{user-id}/orders",
  tag: "orders",
  summary: "Create an order",
  path_params: ["user-id"],
  headers: {},
  content_type: "application/json",
  body: '{"sku":"{{sku}}","qty":1}',
  body_too_long: false,
  created_at: "2026-10-01T00:00:00Z",
  ...over,
});

test("an endpoint fills method, path, headers and body, and the path keeps its template", () => {
  const filled = fromEndpoint(endpoint({}));

  assert.equal(filled.method, "POST");
  assert.equal(filled.path, "/users/{user_id}/orders");
  assert.equal(filled.headers?.["Content-Type"], "application/json");
  assert.equal(filled.body, '{"sku":"{{sku}}","qty":1}');
  assert.equal(filled.label, "Create an order");
});

test("the node it makes passes what the panel checks, and so what Validate checks", () => {
  const filled = fromEndpoint(endpoint({}));
  const problems = requestProblems({ id: "n2", kind: "http_request", ...filled });
  assert.equal(hasProblems(problems), false);
});

test("a header the file gave is kept, and no type is invented for no body", () => {
  const given = fromEndpoint(endpoint({ headers: { "content-type": "application/vnd.api+json" } }));
  assert.deepEqual(Object.keys(given.headers ?? {}), ["content-type"]);

  const bare = fromEndpoint(endpoint({ method: "GET", body: "", content_type: "" }));
  assert.equal(bare.headers, undefined);
  assert.equal(bare.body, "");
});

test("without a summary the label is the method and path", () => {
  assert.equal(fromEndpoint(endpoint({ summary: "  " })).label, "POST /users/{user-id}/orders");
});
