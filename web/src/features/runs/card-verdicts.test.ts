import assert from "node:assert/strict";
import test from "node:test";

import { closeness, parseExpression, verdictsByCard } from "./card-verdicts.ts";
import type { Threshold } from "./types.ts";

const rule = (metric: string, expression: string, passed: boolean): Threshold => ({ metric, expression, passed });

test("the forms of a rule a card can show", () => {
  assert.deepEqual(parseExpression("p(95)<800"), { stat: "p(95)", operator: "<", limit: 800 });
  assert.deepEqual(parseExpression(" rate < 0.01 "), { stat: "rate", operator: "<", limit: 0.01 });
  assert.deepEqual(parseExpression("rate>=0.99"), { stat: "rate", operator: ">=", limit: 0.99 });
  assert.deepEqual(parseExpression("count===10"), { stat: "count", operator: "==", limit: 10 });
  assert.deepEqual(parseExpression("avg<1e3"), { stat: "avg", operator: "<", limit: 1000 });
});

test("what is not that form has no card", () => {
  for (const text of ["", "p(95)", "p(95)<", "p(95)<fast", "p(95)<800 && avg<5", "max<-1", "<800"]) {
    assert.equal(parseExpression(text), null, text);
  }
});

test("a verdict lands on the card of its metric and statistic", () => {
  const found = verdictsByCard([
    rule("http_req_duration", "p(95)<800", false),
    rule("http_req_failed", "rate<0.01", true),
    rule("checks", "rate>0.99", true),
  ]);
  assert.equal(found.p95?.passed, false);
  assert.equal(found.errors?.passed, true);
  assert.equal(found.checks?.rules[0].limit, 0.99);
  assert.equal(found.p99, undefined);
});

test("a rule on one endpoint is not about the run's number", () => {
  assert.deepEqual(verdictsByCard([rule("http_req_duration{name:POST /login}", "p(95)<300", false)]), {});
});

test("a statistic no card shows is left to the list of thresholds", () => {
  assert.deepEqual(verdictsByCard([rule("http_req_duration", "p(90)<400", true), rule("http_req_duration", "avg<5", true)]), {});
});

test("two rules on a card: breached when either is", () => {
  const found = verdictsByCard([rule("http_req_duration", "p(95)<800", true), rule("http_req_duration", "p(95)<500", false)]);
  assert.equal(found.p95?.passed, false);
  assert.equal(found.p95?.rules.length, 2);
});

test("how far a number is toward its limit stays between 0 and 1", () => {
  assert.equal(closeness(400, { operator: "<", limit: 800 }), 0.5);
  assert.equal(closeness(1284, { operator: "<", limit: 800 }), 1);
  assert.equal(closeness(0.048, { operator: "<", limit: 0.01 }), 1);
  assert.equal(closeness(0, { operator: ">", limit: 10 }), 0);
  assert.equal(closeness(null, { operator: "<", limit: 800 }), null);
  assert.equal(closeness(5, { operator: "==", limit: 5 }), null);
  assert.equal(closeness(5, { operator: "<", limit: 0 }), null);
});
