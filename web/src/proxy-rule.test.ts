import assert from "node:assert/strict";
import test from "node:test";

import { decide } from "./proxy-rule.ts";

test("the front door shows the landing to a visitor and the projects to a member", () => {
  assert.equal(decide("/", false), "landing");
  assert.equal(decide("/", true), "projects");
});

test("the legal pages are read by anybody, signed in or not", () => {
  for (const page of ["/privacy", "/terms"]) {
    assert.equal(decide(page, false), "continue");
    assert.equal(decide(page, true), "continue");
  }
});

test("a page that only starts like a legal one still asks for a session", () => {
  assert.equal(decide("/privacy-settings", false), "login");
  assert.equal(decide("/terms/../projects", false), "login");
});

test("every other page asks for a session", () => {
  assert.equal(decide("/projects", false), "login");
  assert.equal(decide("/projects/abc/scenarios", false), "login");
  assert.equal(decide("/projects", true), "continue");
  assert.equal(decide("/runs/abc", true), "continue");
});
