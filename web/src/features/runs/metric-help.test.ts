import assert from "node:assert/strict";
import test from "node:test";

import { errorRateNotice, metricHelp } from "./metric-help.ts";
import { profileHelp } from "./profile-help.ts";
import { profileKinds } from "./types.ts";
import type { Threshold } from "./types.ts";

const labels = [
  "p95 latency",
  "p99 latency",
  "Requests / sec",
  "Error rate",
  "Median latency",
  "Iterations",
  "Checks passed",
  "Data received",
];

test("every card of a run has an explanation", () => {
  for (const label of labels) assert.ok(metricHelp[label]?.length > 20, `${label} has no help`);
});

test("every profile is explained", () => {
  for (const kind of profileKinds) {
    const help = profileHelp[kind];
    assert.ok(help.summary.length > 20 && help.when.length > 10, `${kind} is not explained`);
    // A drawing needs a start and an end, in order, inside the unit square.
    assert.ok(help.shape.length >= 2);
    for (const [at, level] of help.shape) assert.ok(at >= 0 && at <= 1 && level >= 0 && level <= 1);
    assert.deepEqual(help.shape.map(([at]) => at), help.shape.map(([at]) => at).sort((a, b) => a - b));
    assert.equal(help.shape[0][0], 0);
    assert.equal(help.shape.at(-1)?.[0], 1);
  }
});

const threshold = (metric: string): Threshold => ({ metric, expression: "rate<0.01", passed: true });

test("errors with no threshold on them are said", () => {
  assert.ok(errorRateNotice(1, []));
  assert.ok(errorRateNotice(0.2, [threshold("http_req_duration")]));
});

test("no notice when nothing failed, or when a threshold watches failures", () => {
  assert.equal(errorRateNotice(0, []), null);
  assert.equal(errorRateNotice(null, []), null);
  assert.equal(errorRateNotice(1, [threshold("http_req_failed")]), null);
  assert.equal(errorRateNotice(1, [threshold("http_req_failed{name:GET /x}")]), null);
});
