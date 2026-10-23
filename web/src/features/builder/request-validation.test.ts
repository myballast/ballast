import assert from "node:assert/strict";
import { test } from "node:test";

import {
  headerProblems,
  isJson,
  jsonProblem,
  pathProblem,
  queryProblems,
  readsAVariable,
  requestProblems,
  scanJson,
  templateProblem,
} from "./request-validation.ts";

test("valid JSON says nothing, however it is laid out", () => {
  for (const text of [
    '{"a":1}',
    '[1, 2.5, -3e2, true, false, null, "x\\n\\u00e9"]',
    '{\n  "name": "thing",\n  "tags": ["a", {"b": []}]\n}',
    "0",
    '"just a string"',
  ]) {
    assert.equal(scanJson(text), undefined, text);
  }
});

test("a fault is placed on its line and column", () => {
  const problem = jsonProblem('{\n  "a": 1,\n  "b": ,\n  "c": 3\n}');
  assert.equal(problem?.line, 3);
  assert.equal(problem?.column, 8);
  assert.match(problem?.message ?? "", /Expected a value/);
});

test("what is wrong is said in words", () => {
  const cases: [string, RegExp][] = [
    ['{"a": 1,}', /quoted name/],
    ['{"a" 1}', /“:”/],
    ['{"a": 1 "b": 2}', /“,” or “}”/],
    ["[1, 2", /ends before|“,” or “]”/],
    ['{"a": "open', /never closed/],
    ['{"a": 1} trailing', /after the end/],
    ["{'a': 1}", /quoted name/],
    ['{"a": 01}', /“,” or “}”/],
  ];
  for (const [text, says] of cases) assert.match(jsonProblem(text)?.message ?? "no problem", says, text);
});

test("a {{variable}} stands for a value, and every column stays where it was", () => {
  assert.equal(jsonProblem('{"id": {{userId}}, "name": "{{name}}"}'), undefined);

  const problem = jsonProblem('{"id": {{userId}}, "x": }');
  assert.equal(problem?.line, 1);
  assert.equal(problem?.column, 25);
});

test("a body of brackets does not exhaust the stack", () => {
  assert.match(jsonProblem("[".repeat(5000))?.message ?? "", /deeply/);
});

test("the body is held to JSON when it says it is, or looks like it", () => {
  const json = [{ k: "Content-Type", v: "application/json" }];
  assert.equal(isJson("name=a", json), true);
  assert.equal(isJson("name=a", [{ k: "content-type", v: "application/x-www-form-urlencoded" }]), false);
  assert.equal(isJson('{"a":1}', []), true);
  assert.equal(isJson("plain", []), false);
});

test("a form body is not held to JSON", () => {
  const problems = requestProblems({
    id: "a",
    kind: "http_request",
    method: "POST",
    path: "/login",
    body: "user=a&pass={{p}}",
    headers: { "Content-Type": "application/x-www-form-urlencoded" },
  });
  assert.equal(problems.body, undefined);
});

test("the path starts with a slash and its braces name variables", () => {
  assert.equal(pathProblem("/users/{id}/orders"), undefined);
  assert.match(pathProblem("users") ?? "", /slash/);
  assert.match(pathProblem("/users/{user-id}") ?? "", /usable variable name/);
  assert.match(pathProblem("/users/{id") ?? "", /never closed/);
  assert.match(pathProblem(`/${"a".repeat(2001)}`) ?? "", /longer than/);
});

test("a header is named once, whatever its case", () => {
  const found = headerProblems([
    { k: "Accept", v: "a" },
    { k: "accept", v: "b" },
    { k: "", v: "" },
    { k: "", v: "orphan" },
    { k: "X-Id", v: "{{id" },
  ]);
  assert.equal(found[0], "");
  assert.match(found[1], /Already set by header 1/);
  assert.equal(found[2], "");
  assert.match(found[3], /needs a name/);
  assert.match(found[4], /never closed/);
});

test("a credential is read from a variable and never typed", () => {
  const [typed, read, cookie] = headerProblems([
    { k: "Authorization", v: "Bearer abc.def" },
    { k: "Proxy-Authorization", v: "Basic {{basic_auth}}" },
    { k: "Cookie", v: "session=abc" },
  ]);
  assert.match(typed, /read from a secret/);
  assert.equal(read, "");
  assert.match(cookie, /read from a secret/);
  // The message names the fault and never repeats what was typed.
  assert.doesNotMatch(typed, /abc/);
});

test("query parameters need a name, and their values are templates", () => {
  const found = queryProblems([
    { k: "page", v: "{{n}}" },
    { k: "", v: "x" },
    { k: "q", v: "{{" },
  ]);
  assert.equal(found[0], "");
  assert.match(found[1], /needs a name/);
  assert.match(found[2], /never closed/);
});

test("a variable is recognised only when it is well formed", () => {
  assert.equal(readsAVariable("Bearer {{token}}"), true);
  assert.equal(readsAVariable("Bearer {token}"), false);
  assert.equal(readsAVariable("{{bad name}}"), false);
  assert.equal(templateProblem('{"a": 1}'), undefined);
});
