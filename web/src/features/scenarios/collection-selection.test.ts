import assert from "node:assert/strict";
import { test } from "node:test";

import { chosenPositions, firstChoice, groupByFolder, groupFindings, toggleGroup } from "./collection-selection.ts";
import type { CollectionRequest } from "./types.ts";

function request(position: number, name: string, folders: string[] = [], overrides: Partial<CollectionRequest> = {}): CollectionRequest {
  return {
    position,
    folders,
    name,
    method: "GET",
    path: `/${name.toLowerCase()}`,
    has_body: false,
    left_out: false,
    findings: [],
    ...overrides,
  };
}

const collection = [
  request(0, "Login", ["Auth"]),
  request(1, "List", ["Orders"]),
  request(2, "Read", ["Orders", "Details"]),
  request(3, "Ping"),
  request(4, "Logout", ["Auth"]),
];

test("requests are grouped by folder in the order the collection runs them", () => {
  const groups = groupByFolder(collection, "");

  assert.deepEqual(
    groups.map((group) => [group.label, group.requests.map((r) => r.name)]),
    [
      ["Auth", ["Login"]],
      ["Orders", ["List"]],
      ["Orders / Details", ["Read"]],
      ["No folder", ["Ping"]],
      ["Auth", ["Logout"]],
    ],
  );
});

test("a folder met again later is a group of its own, so the order is not rewritten", () => {
  assert.equal(groupByFolder(collection, "").filter((group) => group.label === "Auth").length, 2);
});

test("a search keeps what matches every word, in any field", () => {
  assert.deepEqual(
    groupByFolder(collection, "orders read").flatMap((group) => group.requests.map((r) => r.name)),
    ["Read"],
  );
  assert.deepEqual(groupByFolder(collection, "nothing like this"), []);
});

test("the first choice takes the requests a scenario can hold, up to the limit", () => {
  const withOneLeftOut = [request(0, "A"), request(1, "B", [], { left_out: true }), request(2, "C"), request(3, "D")];

  assert.deepEqual([...firstChoice(withOneLeftOut)], [0, 2, 3]);
  assert.deepEqual([...firstChoice(withOneLeftOut, 2)], [0, 2]);
});

test("chosen positions come out in the order of the collection, whatever order they were ticked in", () => {
  assert.deepEqual(chosenPositions(collection, new Set([4, 0, 2])), [0, 2, 4]);
});

test("a group is ticked whole, then unticked, and a request left out is never ticked", () => {
  const group = { label: "Mixed", requests: [request(0, "A"), request(1, "B", [], { left_out: true }), request(2, "C")] };

  const ticked = toggleGroup(new Set(), group);
  assert.deepEqual([...ticked].sort(), [0, 2]);

  assert.equal(toggleGroup(ticked, group).size, 0);
});

test("findings are gathered by request, in the order they appear, without repeats", () => {
  const groups = groupFindings([
    { request: "", message: "The collection has a script." },
    { request: "Login", message: "has tests of 3 lines, which are not converted" },
    { request: "Login", message: "uses basic authentication" },
    { request: "Login", message: "uses basic authentication" },
    { request: "List", message: "has a formdata body" },
  ]);

  assert.deepEqual(groups, [
    { request: "", messages: ["The collection has a script."] },
    { request: "Login", messages: ["has tests of 3 lines, which are not converted", "uses basic authentication"] },
    { request: "List", messages: ["has a formdata body"] },
  ]);
});
