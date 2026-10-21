import assert from "node:assert/strict";
import { test } from "node:test";

import { chosenInOrder, countWithPathParams, endpointKey, groupByTag, toggleGroup } from "./import-selection.ts";
import type { OpenAPIEndpoint } from "./types.ts";

function endpoint(method: string, path: string, tag = "", summary = ""): OpenAPIEndpoint {
  const params = [...path.matchAll(/\{([^}]+)\}/g)].map((match) => match[1]);
  return { method, path, tag, summary, path_params: params, has_body: false, body_too_long: false };
}

const shop = [
  endpoint("POST", "/orders", "orders"),
  endpoint("GET", "/users/{id}", "users", "Read one user"),
  endpoint("DELETE", "/users/{id}", "users"),
  endpoint("GET", "/health"),
];

test("endpoints are grouped under their tag, in the order the tags appear", () => {
  const groups = groupByTag(shop, "");

  assert.deepEqual(
    groups.map((group) => [group.label, group.endpoints.length]),
    [
      ["orders", 1],
      ["users", 2],
      ["Untagged", 1],
    ],
  );
});

test("a search keeps what matches every word, whatever the case and the field", () => {
  assert.deepEqual(
    groupByTag(shop, "USERS delete").flatMap((group) => group.endpoints.map(endpointKey)),
    ["DELETE /users/{id}"],
  );
  assert.deepEqual(
    groupByTag(shop, "read one").flatMap((group) => group.endpoints.map(endpointKey)),
    ["GET /users/{id}"],
  );
});

test("a search that matches nothing leaves no group", () => {
  assert.deepEqual(groupByTag(shop, "nothing like this"), []);
});

test("a blank search keeps everything", () => {
  assert.equal(groupByTag(shop, "   ").flatMap((group) => group.endpoints).length, 4);
});

test("chosen endpoints come out in the order of the list, whatever order they were ticked in", () => {
  const chosen = new Set([endpointKey(shop[3]), endpointKey(shop[0]), endpointKey(shop[2])]);

  assert.deepEqual(chosenInOrder(shop, chosen), [
    { method: "POST", path: "/orders" },
    { method: "DELETE", path: "/users/{id}" },
    { method: "GET", path: "/health" },
  ]);
});

test("a group is ticked whole, then unticked once it is whole", () => {
  const users = groupByTag(shop, "")[1];

  const ticked = toggleGroup(new Set([endpointKey(shop[1])]), users);
  assert.equal(ticked.size, 2);

  const unticked = toggleGroup(ticked, users);
  assert.equal(unticked.size, 0);
});

test("toggling a group leaves the other choices alone", () => {
  const users = groupByTag(shop, "")[1];
  const ticked = toggleGroup(new Set([endpointKey(shop[0])]), users);

  assert.ok(ticked.has(endpointKey(shop[0])));
  assert.equal(ticked.size, 3);
});

test("requests with a path parameter are counted among the chosen ones only", () => {
  const chosen = new Set([endpointKey(shop[1]), endpointKey(shop[3])]);

  assert.equal(countWithPathParams(shop, chosen), 1);
});
