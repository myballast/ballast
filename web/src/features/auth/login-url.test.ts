import assert from "node:assert/strict";
import { test } from "node:test";

import { loginURL, safeNext } from "./login-url.ts";

// The browser only ever talks to the front end, so a sign-in link is a path on
// it: the front end relays the rest, and the cookie ends up on its host.
test("a sign-in link is a path on this front end, never an address of the API", () => {
  const url = loginURL("github", "/projects?tab=runs");

  assert.equal(url, "/auth/github/login?next=%2Fprojects%3Ftab%3Druns");
  assert.ok(!/^[a-z]+:/i.test(url), "the link names a host");
});

test("a provider name cannot add a path segment", () => {
  assert.equal(loginURL("a/b", "/"), "/auth/a%2Fb/login?next=%2F");
});

test("safeNext keeps a path on this site and nothing else", () => {
  assert.equal(safeNext("/projects/1"), "/projects/1");

  for (const hostile of [
    "https://evil.example",
    "//evil.example",
    "/\\evil.example",
    "javascript:alert(1)",
    "",
    undefined,
    ["/a", "/b"],
  ]) {
    assert.equal(safeNext(hostile), "/", `${String(hostile)} was kept`);
  }
});
