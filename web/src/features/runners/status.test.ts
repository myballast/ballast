import assert from "node:assert/strict";
import { test } from "node:test";

import { outdatedNote, since, statusLabel } from "./status.ts";

test("each status is said with a word, and the word is the API's own", () => {
  assert.equal(statusLabel("online"), "Online");
  assert.equal(statusLabel("draining"), "Draining");
  assert.equal(statusLabel("offline"), "Offline");
});

// The beat is every ten seconds: under five seconds is "now", and the page does
// not pretend to a precision the beat does not have.
test("a beat from a moment ago is just now", () => {
  const now = new Date("2026-10-29T12:00:00Z");
  assert.equal(since("2026-10-29T12:00:00Z", now), "just now");
  assert.equal(since("2026-10-29T11:59:57Z", now), "just now");
});

test("under a minute it is said in seconds", () => {
  const now = new Date("2026-10-29T12:00:00Z");
  assert.equal(since("2026-10-29T11:59:48Z", now), "12 seconds ago");
  assert.equal(since("2026-10-29T11:59:01Z", now), "59 seconds ago");
});

test("then in minutes, hours and days, singular where it is one", () => {
  const now = new Date("2026-10-29T12:00:00Z");
  assert.equal(since("2026-10-29T11:59:00Z", now), "1 minute ago");
  assert.equal(since("2026-10-29T11:15:00Z", now), "45 minutes ago");
  assert.equal(since("2026-10-29T11:00:00Z", now), "1 hour ago");
  assert.equal(since("2026-10-29T06:00:00Z", now), "6 hours ago");
  assert.equal(since("2026-10-28T12:00:00Z", now), "1 day ago");
  assert.equal(since("2026-10-20T12:00:00Z", now), "9 days ago");
});

// A clock a few seconds ahead of this server's must not read as "in the future".
test("a beat that seems to come from the future is just now", () => {
  const now = new Date("2026-10-29T12:00:00Z");
  assert.equal(since("2026-10-29T12:00:05Z", now), "just now");
});

test("a runner that is current says nothing", () => {
  assert.equal(outdatedNote({ outdated: false, contract: 2 }, 2), undefined);
});

// The sentence names both numbers: a refusal that does not is a puzzle, and the
// person reading it is the one who has to fix it.
test("an outdated runner says what it speaks, what is required, and what follows", () => {
  assert.equal(
    outdatedNote({ outdated: true, contract: 1 }, 3),
    "Speaks contract 1, below the minimum of 3: it takes no run until it is updated.",
  );
});
