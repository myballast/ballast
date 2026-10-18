import assert from "node:assert/strict";
import { test } from "node:test";

import { describeDays, describeWindow } from "./window.ts";

test("days are said as ranges, the way the API's refusals say them", () => {
  assert.equal(describeDays(["mon", "tue", "wed", "thu", "fri"]), "mon–fri");
  assert.equal(describeDays(["sat", "sun"]), "sat–sun");
  assert.equal(describeDays(["mon", "wed", "fri"]), "mon, wed, fri");
  assert.equal(describeDays(["fri"]), "fri");
});

test("a range that goes round the end of the week is one range", () => {
  assert.equal(describeDays(["fri", "sat", "sun", "mon"]), "fri–mon");
  assert.equal(describeDays(["sat", "sun", "mon", "tue"]), "sat–tue");
});

test("no days, or all seven, is every day", () => {
  assert.equal(describeDays(undefined), "every day");
  assert.equal(describeDays([]), "every day");
  assert.equal(describeDays(["mon", "tue", "wed", "thu", "fri", "sat", "sun"]), "every day");
});

test("the order the days arrive in does not matter", () => {
  assert.equal(describeDays(["fri", "mon", "wed"]), "mon, wed, fri");
});

test("a window is said with its days, hours and zone, as the design writes it", () => {
  assert.equal(
    describeWindow({ start: "22:00", end: "04:00", timezone: "Europe/Paris", days: ["mon", "tue", "wed", "thu", "fri"] }),
    "mon–fri 22:00–04:00 Europe/Paris",
  );
  assert.equal(describeWindow({ start: "22:00", end: "04:00", timezone: "UTC" }), "22:00–04:00 UTC");
  assert.equal(describeWindow(undefined), "any time");
});
