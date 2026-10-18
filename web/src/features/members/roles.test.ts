import assert from "node:assert/strict";
import { test } from "node:test";

import { displayName, initials, lastAdmins, roleLockedReason, rolesGrantableBy } from "./roles.ts";
import type { Member } from "./types.ts";

const member = (role: Member["role"], id: string = role, name = ""): Member => ({
  user_id: id,
  email: `${id}@acme.test`,
  name,
  role,
  has_custom_permissions: false,
  created_at: "2026-03-01T00:00:00Z",
  last_activity: null,
});

test("nobody can grant a role above their own", () => {
  assert.deepEqual(rolesGrantableBy("viewer"), ["viewer"]);
  assert.deepEqual(rolesGrantableBy("maintainer"), ["viewer", "runner", "maintainer"]);
  assert.deepEqual(rolesGrantableBy("admin"), ["viewer", "runner", "maintainer", "admin"]);
});

test("a lone admin is the last one; two are not", () => {
  const alone = [member("admin", "a"), member("viewer", "v")];
  assert.deepEqual([...lastAdmins(alone)], ["a"]);

  const two = [member("admin", "a"), member("admin", "b")];
  assert.equal(lastAdmins(two).size, 0);
});

test("the role of the last admin is locked, and says why", () => {
  const members = [member("admin", "a"), member("viewer", "v")];
  const last = lastAdmins(members);
  const admin = { canManage: true, role: "admin" as const };

  assert.match(roleLockedReason(members[0], admin, last) ?? "", /at least one admin/);
  assert.equal(roleLockedReason(members[1], admin, last), null);
});

test("without the permission every role is locked", () => {
  const members = [member("admin", "a"), member("viewer", "v")];
  const reason = roleLockedReason(members[1], { canManage: false, role: "maintainer" }, lastAdmins(members));
  assert.match(reason ?? "", /Only an admin/);
});

test("initials come from the name, then from the address", () => {
  assert.equal(initials({ name: "Maëlle Ferrand", email: "m@acme.test" }), "MF");
  assert.equal(initials({ name: "Rin", email: "r@acme.test" }), "RI");
  assert.equal(initials({ name: "", email: "tomas@acme.test" }), "TO");
  assert.equal(initials({ name: "Ada Augusta King", email: "a@acme.test" }), "AK");
});

test("a member with no name is shown by the start of their address", () => {
  assert.equal(displayName({ name: "", email: "j.berg@acme.test" }), "j.berg");
  assert.equal(displayName({ name: "Jonas Berg", email: "j.berg@acme.test" }), "Jonas Berg");
});
