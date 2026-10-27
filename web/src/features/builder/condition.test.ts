import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { test } from "node:test";

import { fromGuided, isReserved, parseCondition, toGuided } from "./condition.ts";

// The list the API's own parser is tested against. A copy of a grammar is only as
// good as the questions it is asked, so both are asked the same ones.
const corpus = JSON.parse(
  readFileSync(new URL("../../../../internal/domain/testdata/conditions.json", import.meta.url), "utf8"),
) as { accepted: string[]; refused: string[] };

test("the corpus is not empty, or agreeing with it would prove nothing", () => {
  assert.ok(corpus.accepted.length > 10);
  assert.ok(corpus.refused.length > 10);
});

test("what the API accepts, the panel accepts", () => {
  for (const expression of corpus.accepted) {
    const parsed = parseCondition(expression);
    assert.equal(parsed.ok, true, `${JSON.stringify(expression)} was refused: ${parsed.ok ? "" : parsed.problem.message}`);
  }
});

test("what the API refuses, the panel refuses", () => {
  for (const expression of corpus.refused) {
    assert.equal(parseCondition(expression).ok, false, `${JSON.stringify(expression)} was accepted`);
  }
});

test("a refusal says where it stops making sense", () => {
  const cases: [string, number, RegExp][] = [
    ["status == 200 &&", 16, /stops in the middle/],
    ["status == 200 ?", 14, /not part of the grammar/],
    ['name == "alice', 8, /not closed/],
    ["(status == 200", 14, /group is not closed/],
    ["a == 1 b", 7, /not expected here/],
  ];
  for (const [text, offset, says] of cases) {
    const parsed = parseCondition(text);
    assert.equal(parsed.ok, false, text);
    if (!parsed.ok) {
      assert.match(parsed.problem.message, says, text);
      assert.equal(parsed.problem.offset, offset, text);
    }
  }
});

test("the variables an expression reads exclude the two the response owns", () => {
  const parsed = parseCondition('code == 200 && response_status == 200 && name == "a" && code < response_time_ms');
  assert.equal(parsed.ok && parsed.variables.join(","), "code,name");
  assert.equal(isReserved("response_status"), true);
  assert.equal(isReserved("status"), false);
});

test("a guided form is read back from an expression, and written the same way", () => {
  for (const text of [
    'status == 200 && name == "alice"',
    "count >= 3 || count < -1",
    "ready",
    "response_status == 200 && response_time_ms < 500",
    "total != 12.5",
  ]) {
    const guided = toGuided(text);
    assert.ok(guided, text);
    assert.equal(fromGuided(guided), text);
  }
});

test("what the form cannot say stays as text", () => {
  assert.equal(toGuided("a == 1 && b == 2 || c == 3"), null);
  assert.equal(toGuided("(a == 1 || b == 2) && c == 3"), null);
  assert.equal(toGuided("a == b"), null);
  assert.equal(toGuided("a =="), null);
});

test("a single clause is joined by all, so the form always has a join to show", () => {
  assert.equal(toGuided("a == 1")?.join, "all");
  assert.equal(toGuided("a == 1 || b == 2")?.join, "any");
});
