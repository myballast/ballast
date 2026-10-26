import assert from "node:assert/strict";
import { test } from "node:test";

import type { ScenarioGraph } from "../scenarios/types.ts";
import {
  applySuggestion,
  bindSecretBefore,
  suggestionContext,
  undefinedNames,
  unboundSecrets,
  variableForKey,
  variablesBefore,
  variablesRead,
} from "./variables.ts";

const graph: ScenarioGraph = {
  start: "bind",
  nodes: [
    { id: "bind", kind: "set_variable", variable: "token", from_env: "API_TOKEN" },
    { id: "login", kind: "http_request", method: "POST", path: "/login" },
    { id: "take", kind: "extract_variable", variable: "userId", from: "$.id" },
    { id: "me", kind: "http_request", method: "GET", path: "/users/{userId}" },
    { id: "later", kind: "extract_variable", variable: "afterwards", from: "$.x" },
    { id: "done", kind: "end" },
  ],
  edges: [
    { from: "bind", to: "login" },
    { from: "login", to: "take" },
    { from: "take", to: "me" },
    { from: "me", to: "later" },
    { from: "later", to: "done" },
  ],
};

test("a node reads what the nodes before it wrote, nearest first, and nothing after", () => {
  assert.deepEqual(variablesBefore(graph, "me").map((v) => v.name), ["userId", "token"]);
  assert.deepEqual(variablesBefore(graph, "bind"), []);
});

test("a variable bound to a secret says so and carries no value", () => {
  const token = variablesBefore(graph, "me").find((v) => v.name === "token");
  assert.equal(token?.secret, true);
  assert.deepEqual(Object.keys(token ?? {}).sort(), ["from", "name", "secret"]);
});

test("a loop that leads back does not make the walk run forever", () => {
  const loop: ScenarioGraph = {
    start: "a",
    nodes: [
      { id: "a", kind: "extract_variable", variable: "x", from: "$.x" },
      { id: "b", kind: "http_request", method: "GET", path: "/" },
    ],
    edges: [
      { from: "a", to: "b" },
      { from: "b", to: "a" },
    ],
  };
  assert.deepEqual(variablesBefore(loop, "b").map((v) => v.name), ["x"]);
});

test("a secret not yet read by any node is offered to be bound", () => {
  assert.deepEqual(unboundSecrets(["API_TOKEN", "CLIENT_SECRET"], graph), ["CLIENT_SECRET"]);
});

test("a variable name is made from the key and never taken twice", () => {
  assert.equal(variableForKey("API_TOKEN", []), "api_token");
  assert.equal(variableForKey("API_TOKEN", ["api_token"]), "api_token_2");
  assert.equal(variableForKey("2FA-KEY", []), "_2fa_key");
});

test("binding puts the secret before the request and keeps the way in", () => {
  const { graph: next, variable } = bindSecretBefore(graph, "login", "CLIENT_SECRET", "n9");

  assert.equal(variable, "client_secret");
  const into = next.edges.filter((e) => e.to === "login");
  assert.deepEqual(into, [{ from: "n9", to: "login" }]);
  assert.ok(next.edges.some((e) => e.from === "bind" && e.to === "n9"));
  assert.equal(next.nodes.find((n) => n.id === "n9")?.from_env, "CLIENT_SECRET");
  assert.equal(next.nodes.find((n) => n.id === "n9")?.value, undefined);
  assert.equal(next.start, "bind");
});

test("binding before the first request makes the binding the start", () => {
  const { graph: next } = bindSecretBefore(graph, "bind", "OTHER", "n9");
  assert.equal(next.start, "n9");
  assert.deepEqual(next.edges.find((e) => e.from === "n9"), { from: "n9", to: "bind" });
});

test("a variable nobody wrote is named, once", () => {
  const request = { id: "r", kind: "http_request" as const, method: "GET", path: "/a/{ghost}", body: '{"x":"{{ghost}}","y":"{{userId}}"}' };
  assert.deepEqual(undefinedNames(request, [{ name: "userId", secret: false, from: "take" }]), ["ghost"]);
});

test("typing {{ opens the suggestions, and a closed one does not", () => {
  assert.deepEqual(suggestionContext("Bearer {{to", 11), { from: 7, query: "to" });
  assert.deepEqual(suggestionContext("{{", 2), { from: 0, query: "" });
  assert.equal(suggestionContext("Bearer {{token}} x", 18), null);
  assert.equal(suggestionContext("no braces", 5), null);
  assert.equal(suggestionContext("{{a b", 5), null);
});

test("choosing writes the whole placeholder and puts the caret after it", () => {
  const context = suggestionContext("Bearer {{to", 11);
  assert.deepEqual(applySuggestion("Bearer {{to", 11, context!, "token"), { text: "Bearer {{token}}", caret: 16 });
  assert.deepEqual(applySuggestion("a {{to}} b", 6, suggestionContext("a {{to}} b", 6)!, "token"), {
    text: "a {{token}} b",
    caret: 11,
  });
});

test("every place a node can read a variable is found, once each", () => {
  assert.deepEqual(
    variablesRead({
      id: "r",
      kind: "http_request",
      method: "GET",
      path: "/users/{id}/orders/{id}",
      headers: { "X-A": "{{a}}", "X-B": "n {{b}} {{a}}" },
      query: [{ name: "q", value: "{{c}}" }],
      body: '{"x": "{{d}}"}',
    }).sort(),
    ["a", "b", "c", "d", "id"],
  );
  assert.deepEqual(variablesRead({ id: "s", kind: "set_variable", variable: "v", value: "{{src}}" }), ["src"]);
  assert.deepEqual(variablesRead({ id: "s", kind: "set_variable", variable: "v", from_env: "KEY" }), []);
});

test("a condition reads the names in it, and not the text between quotes", () => {
  assert.deepEqual(variablesRead({ id: "c", kind: "condition", expression: 'status == 200 && name == "alice smith"' }).sort(), [
    "name",
    "status",
  ]);
  assert.deepEqual(variablesRead({ id: "a", kind: "assertion", expression: "count >= 3" }), ["count"]);
  assert.deepEqual(variablesRead({ id: "t", kind: "think_time", seconds: 1 }), []);
});

test("a variable read before it is written is named, for any kind of node", () => {
  const known = [{ name: "token", secret: true, from: "bind" }];
  assert.deepEqual(undefinedNames({ id: "c", kind: "condition", expression: "token == ghost" }, known), ["ghost"]);
  assert.deepEqual(undefinedNames({ id: "s", kind: "set_variable", variable: "v", value: "{{ghost}}" }, known), ["ghost"]);
  assert.deepEqual(undefinedNames({ id: "c", kind: "condition", expression: "token == 1" }, known), []);
});
