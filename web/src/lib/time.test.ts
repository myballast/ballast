import assert from "node:assert/strict";
import { test } from "node:test";

import { formatFullMoment, formatMoment } from "./time.ts";

// 2026-10-18T12:02:11Z is 14:02 in Paris (summer time) and 08:02 in New York.
const iso = "2026-10-18T12:02:11Z";

test("a moment is written the same way everywhere, in the zone it is asked for", () => {
  assert.equal(formatMoment(iso, "UTC"), "18 Oct 2026, 12:02");
  assert.equal(formatMoment(iso, "Europe/Paris"), "18 Oct 2026, 14:02");
  assert.equal(formatMoment(iso, "America/New_York"), "18 Oct 2026, 08:02");
});

test("the zone can move the date, not only the hour", () => {
  assert.equal(formatMoment("2026-10-18T23:30:00Z", "Europe/Paris"), "19 Oct 2026, 01:30");
});

test("the tooltip carries the weekday, the seconds and the zone", () => {
  const full = formatFullMoment(iso, "Europe/Paris");
  assert.match(full, /Sunday/);
  assert.match(full, /14:02:11/);
  assert.match(full, /CEST|GMT\+2/);
});

test("midnight is 00:00, not 24:00", () => {
  assert.equal(formatMoment("2026-10-18T00:00:00Z", "UTC"), "18 Oct 2026, 00:00");
});
