import assert from "node:assert/strict";
import { test } from "node:test";

import { levelOf, withoutLevel } from "./log-level.ts";

test("a line that starts with its level has that level", () => {
  assert.equal(levelOf("ERROR POST /api/checkout 503 in 4 021ms"), "error");
  assert.equal(levelOf("WARN  connection pool saturated"), "warn");
  assert.equal(levelOf("  ERROR indented"), "error");
});

test("k6's own formats are read", () => {
  assert.equal(levelOf('time="2026-10-16T14:06:06Z" level=error msg="thresholds failed"'), "error");
  assert.equal(levelOf('time="2026-10-16T14:06:06Z" level=warning msg="slow"'), "warn");
  assert.equal(levelOf("ERRO[0006] GoError: request timeout"), "error");
  assert.equal(levelOf("WARN[0004] The test has generated metrics"), "warn");
});

test("the level is not found in the middle of a sentence", () => {
  assert.equal(levelOf("INFO  0 errors in 18 204 iterations"), null);
  assert.equal(levelOf("INFO  authorization error rate checked"), null);
  assert.equal(levelOf("INFO  a warning about nothing"), null);
});

test("a plain line has no level", () => {
  assert.equal(levelOf("INFO  POST /api/cart/items 201 in 284ms"), null);
  assert.equal(levelOf(""), null);
});

test("the level is not case sensitive where k6 writes it", () => {
  assert.equal(levelOf("level=ERROR msg=x"), "error");
  assert.equal(levelOf("Error: something"), "error");
});

test("the level at the start of a line is taken off the text, so it is not said twice", () => {
  assert.equal(withoutLevel("ERROR POST /api/checkout 503"), "POST /api/checkout 503");
  assert.equal(withoutLevel("WARN  connection pool saturated"), "connection pool saturated");
  assert.equal(withoutLevel("ERRO[0006] GoError: timeout"), "GoError: timeout");
});

test("a level inside the line, and a line without one, stay as written", () => {
  assert.equal(withoutLevel('time="x" level=error msg="boom"'), 'time="x" level=error msg="boom"');
  assert.equal(withoutLevel("INFO  POST /api/cart/items 201"), "INFO  POST /api/cart/items 201");
  assert.equal(withoutLevel("ERROR"), "ERROR");
});
