import assert from "node:assert/strict";
import { test } from "node:test";

import type { GraphNode } from "../scenarios/types.ts";
import { expressionProblem, loopProblem, nodeProblem, pauseProblem, setVariableProblem } from "./node-validation.ts";

const node = (over: Partial<GraphNode>): GraphNode => ({ id: "n", kind: "loop", ...over });

test("a condition that does not parse is said in the panel, with where", () => {
  assert.equal(expressionProblem("response_status == 200"), undefined);
  assert.match(expressionProblem("status ==") ?? "", /Character 10: .*stops in the middle/);
  assert.match(expressionProblem("fetch('x')") ?? "", /Character \d+/);
  assert.match(expressionProblem("") ?? "", /empty/);
  assert.match(nodeProblem({ id: "c", kind: "assertion", expression: "a ==" }) ?? "", /stops in the middle/);
});

test("a loop with no maximum is refused, over a list as well as a count", () => {
  assert.equal(loopProblem(node({ iterations: 3 })), undefined);
  assert.equal(loopProblem(node({ iterations: 1000 })), undefined);
  assert.equal(loopProblem(node({ over: "items", item: "item", iterations: 20 })), undefined);

  assert.match(loopProblem(node({})) ?? "", /maximum/);
  assert.match(loopProblem(node({ iterations: 0 })) ?? "", /maximum/);
  assert.match(loopProblem(node({ iterations: 1001 })) ?? "", /maximum/);
  assert.match(loopProblem(node({ iterations: 1.5 })) ?? "", /maximum/);
  assert.match(loopProblem(node({ over: "items", item: "item" })) ?? "", /maximum/);
});

test("a list needs its element named, and the other way round", () => {
  assert.match(loopProblem(node({ iterations: 5, over: "items" })) ?? "", /Name the variable/);
  assert.match(loopProblem(node({ iterations: 5, item: "item" })) ?? "", /Choose the list/);
  assert.match(loopProblem(node({ iterations: 5, over: "items", item: "items" })) ?? "", /same name/);
  assert.match(loopProblem(node({ iterations: 5, over: "items", item: "response_status" })) ?? "", /last response/);
  assert.match(loopProblem(node({ iterations: 5, over: "a b", item: "x" })) ?? "", /not a variable name/);
});

test("a pause is a time, or a range inside 5 minutes", () => {
  const pause = (seconds?: number, max?: number) => pauseProblem(node({ kind: "think_time", seconds, seconds_max: max }));

  assert.equal(pause(2), undefined);
  assert.equal(pause(1, 5), undefined);
  assert.equal(pause(300, 300), undefined);
  assert.match(pause(0) ?? "", /more than 0/);
  assert.match(pause(undefined) ?? "", /more than 0/);
  assert.match(pause(301) ?? "", /at most 300/);
  assert.match(pause(5, 1) ?? "", /longest wait/);
  assert.match(pause(1, 301) ?? "", /longest wait/);
});

test("a variable is set to a value or to a secret, never both, and not under a reserved name", () => {
  const set = (over: Partial<GraphNode>) => setVariableProblem({ id: "s", kind: "set_variable", variable: "token", ...over });

  assert.equal(set({ value: "abc {{other}}" }), undefined);
  assert.equal(set({ from_env: "API_TOKEN" }), undefined);

  assert.match(set({ variable: "1x" }) ?? "", /variable name/);
  assert.match(set({ variable: "response_status" }) ?? "", /last response/);
  assert.match(set({ value: "{{" }) ?? "", /never closed/);
  assert.match(set({ value: "x", from_env: "API_TOKEN" }) ?? "", /not both/);
  assert.match(set({ from_env: "api_token" }) ?? "", /upper case/);
  assert.match(set({ from_env: "K6_VUS" }) ?? "", /K6_/);
  assert.match(set({ from_env: "BASE_URL" }) ?? "", /BASE_URL/);
});

test("the other kinds have nothing to say", () => {
  assert.equal(nodeProblem({ id: "e", kind: "end" }), undefined);
  assert.equal(nodeProblem({ id: "r", kind: "http_request", method: "GET", path: "x" }), undefined);
});
