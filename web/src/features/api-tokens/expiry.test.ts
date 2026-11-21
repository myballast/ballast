import assert from "node:assert/strict";
import { test } from "node:test";

import { defaultExpiryDays, expiresAtFor, expiryOptions, parseExpiryDays } from "./expiry.ts";

test("an expiry is that many whole days from now, in UTC", () => {
  const now = new Date("2026-11-20T12:00:00.000Z");

  assert.equal(expiresAtFor(30, now), "2026-12-20T12:00:00.000Z");
  assert.equal(expiresAtFor(90, now), "2027-02-18T12:00:00.000Z");
});

test("the default is one of the choices, and is the one the API applies when none is given", () => {
  assert.equal(defaultExpiryDays, 90);
  assert.ok(expiryOptions.some((option) => option.days === defaultExpiryDays));
});

test("only a listed number of days is accepted from the form", () => {
  for (const option of expiryOptions) {
    assert.equal(parseExpiryDays(String(option.days)), option.days);
  }

  for (const raw of ["", "0", "-30", "9999", "abc", "30.5", " 30", "1e2", "NaN"]) {
    assert.equal(parseExpiryDays(raw), null, `"${raw}" was accepted`);
  }
});

// The API refuses anything past a year from its own clock, and this server's
// clock is not that one: the longest choice stays a day short, so that a clock
// a little ahead never turns "1 year" into a refusal.
test("the longest choice stays under a year", () => {
  const longest = Math.max(...expiryOptions.map((option) => option.days));

  assert.ok(longest < 365);
  assert.equal(expiryOptions.find((option) => option.days === longest)?.label, "1 year");
});

test("the choices are listed shortest first, each with a different label", () => {
  const days = expiryOptions.map((option) => option.days);
  assert.deepEqual(days, [...days].sort((a, b) => a - b));

  const labels = expiryOptions.map((option) => option.label);
  assert.equal(new Set(labels).size, labels.length);
});
