import assert from "node:assert/strict";
import test from "node:test";

import { noteOf, suggestEndpoints } from "./endpoint-suggest.ts";
import type { CatalogEndpoint } from "../endpoints/types.ts";

const endpoint = (method: string, path: string, summary = "", extra: Partial<CatalogEndpoint> = {}): CatalogEndpoint => ({
  id: `${method}${path}`,
  source: "spec",
  method,
  path,
  tag: "",
  summary,
  path_params: [],
  headers: {},
  content_type: "",
  body: "",
  body_too_long: false,
  created_at: "",
  ...extra,
});

const catalog = [
  endpoint("GET", "/health", "Liveness check"),
  endpoint("POST", "/auth/login", "Log in with the single test user"),
  endpoint("GET", "/me", "Current user"),
  endpoint("GET", "/products/{id}", "One product"),
  endpoint("GET", "/products", "List products"),
];

test("typing a path offers the endpoints that start with it first", () => {
  assert.deepEqual(suggestEndpoints(catalog, "/pro").map((e) => e.path), ["/products/{id}", "/products"]);
  assert.deepEqual(suggestEndpoints(catalog, "/auth").map((e) => e.path), ["/auth/login"]);
});

test("a word may match anywhere, and the path match comes first", () => {
  // "user" is in two summaries and in no path: both are offered.
  assert.deepEqual(suggestEndpoints(catalog, "user").map((e) => e.path), ["/auth/login", "/me"]);
  assert.equal(suggestEndpoints(catalog, "login")[0].path, "/auth/login");
});

test("every word has to fit, in any order", () => {
  assert.deepEqual(suggestEndpoints(catalog, "post login").map((e) => e.path), ["/auth/login"]);
  assert.deepEqual(suggestEndpoints(catalog, "login post").map((e) => e.path), ["/auth/login"]);
  assert.deepEqual(suggestEndpoints(catalog, "get login"), []);
});

test("nothing typed offers the first few, and never more than the limit", () => {
  assert.equal(suggestEndpoints(catalog, "").length, 5);
  assert.equal(suggestEndpoints(catalog, "", 2).length, 2);
  assert.deepEqual(suggestEndpoints(catalog, "   ").length, 5);
});

test("case does not matter", () => {
  assert.deepEqual(suggestEndpoints(catalog, "/AUTH").map((e) => e.path), ["/auth/login"]);
});

test("what a pick will not bring is said", () => {
  assert.equal(noteOf(endpoint("POST", "/x", "", { content_type: "application/json", body: "" })), "no example body in the file");
  assert.equal(noteOf(endpoint("POST", "/x", "", { content_type: "application/json", body: "{}" })), null);
  assert.equal(noteOf(endpoint("GET", "/x")), null);
  assert.equal(noteOf(endpoint("POST", "/x", "", { body_too_long: true })), "example body too long to copy");
});
