import assert from "node:assert/strict";
import test from "node:test";

import { collapsedFor } from "./sidebar-state.ts";

test("a choice wins over the page", () => {
  assert.equal(collapsedFor("open", "/projects/p/scenarios/s/builder"), false);
  assert.equal(collapsedFor("collapsed", "/projects"), true);
});

test("with no choice the builder starts collapsed and nothing else does", () => {
  assert.equal(collapsedFor(undefined, "/projects/p/scenarios/s/builder"), true);
  assert.equal(collapsedFor(undefined, "/projects/p/scenarios/s/builder/"), true);
  assert.equal(collapsedFor(undefined, "/projects/p/scenarios/s"), false);
  assert.equal(collapsedFor(undefined, "/projects"), false);
});
