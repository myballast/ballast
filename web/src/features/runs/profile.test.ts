import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { test } from "node:test";

import { clampProfile, duration, isLowered, paramsProblem, sameParams, stagesOf, summaryOf } from "./profile.ts";
import type { ProfileParams } from "./types.ts";

const params = (target_vus: number, ramp_up_s: number, plateau_s: number, ramp_down_s: number): ProfileParams => ({
  target_vus,
  ramp_up_s,
  plateau_s,
  ramp_down_s,
});

// The questions the API's own clamp is asked, so that what a person is told before a
// run is what the run is given.
const corpus = JSON.parse(
  readFileSync(new URL("../../../../internal/domain/testdata/clamp.json", import.meta.url), "utf8"),
) as { cases: { name: string; params: ProfileParams; ceiling_vus: number; ceiling_s: number; want: ProfileParams }[] };

test("the corpus is not empty, or agreeing with it would prove nothing", () => {
  assert.ok(corpus.cases.length >= 5);
});

test("the page lowers a profile as the API does", () => {
  for (const c of corpus.cases) {
    assert.deepEqual(clampProfile(c.params, { vus: c.ceiling_vus, seconds: c.ceiling_s }), c.want, c.name);
  }
});

test("clamping only ever lowers", () => {
  const low = params(1, 0, 1, 0);
  assert.deepEqual(clampProfile(low, { vus: 100, seconds: 600 }), low);
  assert.equal(isLowered(low, clampProfile(low, { vus: 100, seconds: 600 })), false);
  assert.equal(isLowered(params(400, 0, 60, 0), clampProfile(params(400, 0, 60, 0), { vus: 100, seconds: 600 })), true);
});

test("a profile is a climb, a plateau and a descent, each only when it lasts", () => {
  assert.deepEqual(
    stagesOf(params(100, 30, 60, 30)).map((s) => [s.name, s.seconds, s.from, s.to]),
    [
      ["ramp up", 30, 0, 100],
      ["plateau", 60, 100, 100],
      ["ramp down", 30, 100, 0],
    ],
  );
  assert.deepEqual(stagesOf(params(100, 0, 60, 30)).map((s) => s.name), ["plateau", "ramp down"]);
  assert.deepEqual(stagesOf(params(100, 30, 0, 0)).map((s) => s.name), ["ramp up"]);
});

test("with no ramp at either end the load is flat, and one stage says so", () => {
  assert.deepEqual(stagesOf(params(5, 0, 60, 0)), [{ name: "steady", seconds: 60, from: 5, to: 5 }]);
  assert.deepEqual(stagesOf(params(5, 0, 0, 0)), []);
});

test("the summary says the stages, the peak and the length", () => {
  assert.equal(summaryOf(params(100, 30, 60, 30)), "3 stages · peak 100 VUs · 2m 00s");
  assert.equal(summaryOf(params(1, 0, 45, 0)), "1 stage · peak 1 VU · 45s");
});

test("a duration is written the way it is read", () => {
  assert.equal(duration(45), "45s");
  assert.equal(duration(60), "1m 00s");
  assert.equal(duration(125), "2m 05s");
  assert.equal(duration(3600), "1h 00m");
  assert.equal(duration(3900), "1h 05m");
});

test("parameters that describe no test are said, as the API says them", () => {
  assert.equal(paramsProblem(params(10, 5, 20, 5)), undefined);
  assert.equal(paramsProblem(params(1, 0, 1, 0)), undefined);
  assert.match(paramsProblem({ target_vus: 10, ramp_up_s: 5 }) ?? "", /whole number/);
  assert.match(paramsProblem(params(0, 5, 20, 5)) ?? "", /virtual user/);
  assert.match(paramsProblem(params(5, -1, 20, 5)) ?? "", /negative/);
  assert.match(paramsProblem(params(5, 0, 0, 0)) ?? "", /no time/);
  assert.match(paramsProblem({ ...params(5, 0, 0, 0), plateau_s: 1.5 }) ?? "", /whole number/);
});

test("two profiles are the same when every field is", () => {
  assert.equal(sameParams(params(1, 2, 3, 4), params(1, 2, 3, 4)), true);
  assert.equal(sameParams(params(1, 2, 3, 4), params(1, 2, 3, 5)), false);
  assert.equal(sameParams(params(1, 2, 3, 4), { target_vus: 1 }), false);
});
