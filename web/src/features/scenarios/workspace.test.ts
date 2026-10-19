import assert from "node:assert/strict";
import { test } from "node:test";

import { isDirty, profileSummary, runBlockedReason, type Draft } from "./workspace.ts";

const saved: Draft = {
  name: "checkout",
  script: "export default function () {}",
  profile: "load",
  params: { target_vus: 10, ramp_up_s: 30, plateau_s: 60, ramp_down_s: 30 },
};

test("the profile is said in a few words", () => {
  assert.equal(profileSummary("smoke", { target_vus: 1, ramp_up_s: 0, plateau_s: 60, ramp_down_s: 0 }), "smoke · 1 VU · 60 s");
  assert.equal(profileSummary("load", { target_vus: 10, ramp_up_s: 30, plateau_s: 60, ramp_down_s: 30 }), "load · 10 VUs · 2 min");
  assert.equal(profileSummary("soak", { target_vus: 400, ramp_up_s: 60, plateau_s: 360, ramp_down_s: 60 }), "soak · 400 VUs · 8 min");
});

test("a duration that is not whole minutes stays in seconds", () => {
  assert.equal(profileSummary("load", { target_vus: 2, ramp_up_s: 0, plateau_s: 150, ramp_down_s: 5 }), "load · 2 VUs · 155 s");
});

test("an untouched form is not dirty", () => {
  assert.equal(isDirty(saved, { ...saved, params: { ...saved.params } }), false);
});

test("any field that moved makes it dirty", () => {
  assert.equal(isDirty(saved, { ...saved, name: "checkout-v2" }), true);
  assert.equal(isDirty(saved, { ...saved, script: saved.script + "\n" }), true);
  assert.equal(isDirty(saved, { ...saved, profile: "stress" }), true);
  assert.equal(isDirty(saved, { ...saved, params: { ...saved.params, plateau_s: 61 } }), true);
});

test("a parameter emptied is a change", () => {
  assert.equal(isDirty(saved, { ...saved, params: { ...saved.params, target_vus: undefined } }), true);
});

test("a change put back is no change", () => {
  const edited = { ...saved, name: "x" };
  assert.equal(isDirty(saved, edited), true);
  assert.equal(isDirty(saved, { ...edited, name: "checkout" }), false);
});

test("run is blocked for the reason that matters first", () => {
  assert.match(runBlockedReason({ dirty: true, canRun: false, verifiedEnvironments: 0 }) ?? "", /role/);
  assert.match(runBlockedReason({ dirty: true, canRun: true, verifiedEnvironments: 0 }) ?? "", /Save your changes/);
  assert.match(runBlockedReason({ dirty: false, canRun: true, verifiedEnvironments: 0 }) ?? "", /No environment is verified/);
  assert.equal(runBlockedReason({ dirty: false, canRun: true, verifiedEnvironments: 2 }), null);
});
