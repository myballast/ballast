import assert from "node:assert/strict";
import test from "node:test";

import { barHelp, fieldHelp, howItWorks, kindHelp, shortcuts } from "./help.ts";
import { nodeKinds } from "../scenarios/types.ts";

test("every kind of step is explained", () => {
  for (const kind of nodeKinds) assert.ok(kindHelp[kind].length > 30, kind);
});

test("every field and every part of the bar says something", () => {
  for (const [name, text] of [...Object.entries(fieldHelp), ...Object.entries(barHelp)]) {
    assert.ok(text.length > 30, name);
  }
});

test("the guide has its steps and its shortcuts", () => {
  assert.equal(howItWorks.length, 4);
  assert.ok(shortcuts.every((s) => s.keys.length > 0 && s.does.length > 0));
});
