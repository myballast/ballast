import assert from "node:assert/strict";
import test from "node:test";

import { decide } from "./proxy-rule.ts";

test("the front door shows the landing to a visitor and the projects to a member", () => {
  assert.equal(decide("/", false), "landing");
  assert.equal(decide("/", true), "projects");
});

test("every other page asks for a session", () => {
  assert.equal(decide("/projects", false), "login");
  assert.equal(decide("/projects/abc/scenarios", false), "login");
  assert.equal(decide("/projects", true), "continue");
  assert.equal(decide("/runs/abc", true), "continue");
});
