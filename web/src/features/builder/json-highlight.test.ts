import assert from "node:assert/strict";
import { test } from "node:test";

import { tokenizeJsonLine } from "./json-highlight.ts";

const kinds = (line: string) => tokenizeJsonLine(line).map((t) => `${t.kind}:${t.text}`);

test("names, strings, numbers and literals are told apart", () => {
  assert.deepEqual(kinds('  "id": 42, "ok": true, "x": null'), [
    "plain:  ",
    'key:"id"',
    "punctuation::",
    "plain: ",
    "number:42",
    "punctuation:,",
    "plain: ",
    'key:"ok"',
    "punctuation::",
    "plain: ",
    "literal:true",
    "punctuation:,",
    "plain: ",
    'key:"x"',
    "punctuation::",
    "plain: ",
    "literal:null",
  ]);
});

test("a variable is its own kind, in a value or in a string", () => {
  assert.ok(kinds('{"id": {{userId}}}').includes("variable:{{userId}}"));
  assert.ok(kinds('"Bearer {{token}}"').some((k) => k.startsWith("string:")));
});

test("the tokens put the line back together, whatever it holds", () => {
  for (const line of ['{"a": [1, 2.5e3, "x\\"y"]}', "", "   ", 'half "open', "not json at all {{", '"é": "ü"']) {
    assert.equal(tokenizeJsonLine(line).map((t) => t.text).join(""), line);
  }
});
