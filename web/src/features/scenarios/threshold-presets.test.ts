import assert from "node:assert/strict";
import test from "node:test";

import { buildRule, endpointChoices, isAdded, measures, presets, ruleOf, type MeasureId } from "./threshold-presets.ts";
import type { ScenarioGraph } from "./types.ts";

test("the presets are the rules they say", () => {
  const rules = Object.fromEntries(presets.map((p) => [p.id, ruleOf(p)]));
  assert.deepEqual(rules.fast, { metric: "http_req_duration", expression: "p(95)<500" });
  assert.deepEqual(rules.reliable, { metric: "http_req_failed", expression: "rate<0.01" });
  assert.deepEqual(rules.checks, { metric: "checks", expression: "rate>0.99" });
  assert.deepEqual(rules.outliers, { metric: "http_req_duration", expression: "p(99)<1500" });
  for (const p of presets) assert.ok(p.help.length > 40, p.id);
});

test("a percentage is written as k6 reads it, without float noise", () => {
  const built = buildRule({ measure: "checks", stat: "rate", value: 99 });
  assert.ok(built.ok && built.expression === "rate>0.99");
  const odd = buildRule({ measure: "failed", stat: "rate", value: 7 });
  assert.ok(odd.ok && odd.expression === "rate<0.07");
});

test("a rule can be narrowed to one request", () => {
  const built = buildRule({ measure: "response_time", stat: "p(95)", value: 300, endpoint: "POST /login" });
  assert.ok(built.ok);
  assert.equal(built.metric, "http_req_duration{name:POST /login}");
  assert.equal(built.expression, "p(95)<300");
  assert.match(built.sentence, /POST \/login/);
});

test("checks have no endpoint, so the choice is ignored", () => {
  const built = buildRule({ measure: "checks", stat: "rate", value: 95, endpoint: "GET /x" });
  assert.ok(built.ok && built.metric === "checks");
});

test("a request whose path holds a variable cannot be selected", () => {
  for (const endpoint of ["GET /users/{id}", "GET /a}b", "GET /a\nb"]) {
    assert.equal(buildRule({ measure: "response_time", stat: "avg", value: 100, endpoint }).ok, false, endpoint);
  }
});

test("numbers that make no rule are refused", () => {
  for (const value of [0, -1, NaN, Infinity]) {
    assert.equal(buildRule({ measure: "response_time", stat: "avg", value }).ok, false, String(value));
  }
  assert.equal(buildRule({ measure: "failed", stat: "rate", value: 101 }).ok, false);
  assert.equal(buildRule({ measure: "response_time", stat: "nope", value: 10 }).ok, false);
});

test("every measure starts from a rule that builds", () => {
  for (const id of Object.keys(measures) as MeasureId[]) {
    const m = measures[id];
    assert.ok(buildRule({ measure: id, stat: m.stats[0].id, value: m.start }).ok, id);
  }
});

test("a rule already stored is recognised", () => {
  const stored = [{ id: "1", metric: "checks", expression: "rate>0.99" }];
  assert.equal(isAdded(stored as never, ruleOf(presets[2])), true);
  assert.equal(isAdded(stored as never, ruleOf(presets[0])), false);
});

test("the requests of a graph are listed once, by their tag", () => {
  const graph = {
    start: "a",
    nodes: [
      { id: "a", kind: "http_request", method: "POST", path: "/login" },
      { id: "b", kind: "http_request", method: "POST", path: "/login" },
      { id: "c", kind: "http_request", method: "GET", path: "/users/{id}" },
      { id: "d", kind: "end" },
    ],
    edges: [],
  } as unknown as ScenarioGraph;

  assert.deepEqual(endpointChoices(graph), [
    { name: "POST /login", usable: true },
    { name: "GET /users/{id}", usable: false },
  ]);
  assert.deepEqual(endpointChoices(undefined), []);
});
