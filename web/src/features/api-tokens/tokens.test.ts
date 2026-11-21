import assert from "node:assert/strict";
import { test } from "node:test";

import { creditsLeft, liveTokens, whoMade } from "./tokens.ts";
import type { ApiToken } from "./types.ts";

const token = (state: ApiToken["state"], extra: Partial<ApiToken> = {}): ApiToken => ({
  id: state + Math.random(),
  name: state,
  prefix: "blt_Ab3dEf9h",
  state,
  expires_at: "2027-01-01T00:00:00Z",
  created_at: "2026-11-01T00:00:00Z",
  ...extra,
});

test("what counts against the ceiling is what has not been revoked, expired or not", () => {
  const tokens = [token("active"), token("expired"), token("revoked"), token("active")];

  assert.equal(liveTokens(tokens), 3);
});

test("the places left are never negative, and none are left at the ceiling", () => {
  const tokens = [token("active"), token("active"), token("revoked")];

  assert.equal(creditsLeft(tokens, 20), 18);
  assert.equal(creditsLeft(tokens, 2), 0);
  assert.equal(creditsLeft(tokens, 1), 0);
});

test("who made a token is said in words, and never by an identifier", () => {
  assert.equal(whoMade(token("active", { created_by: "u1", created_by_name: "Ada Lovelace" })), "Ada Lovelace");
  // A person with no name on file is still a person.
  assert.equal(whoMade(token("active", { created_by: "u1" })), "a member");
  // The author left: the foreign key cleared the id, and the name with it.
  assert.equal(whoMade(token("active")), "a former member");
});
