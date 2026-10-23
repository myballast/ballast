import assert from "node:assert/strict";
import { test } from "node:test";

import { defaultHost, defaultKinds, hostCounts, kindCounts, visibleRequests } from "./har-filter.ts";
import type { CollectionRequest, RequestKind } from "./types.ts";

function request(position: number, host: string, kind: RequestKind): CollectionRequest {
  return {
    position,
    folders: [],
    name: `GET /${position}`,
    method: "GET",
    host,
    kind,
    path: `/${position}`,
    has_body: false,
    left_out: false,
    findings: [],
  };
}

const recording = [
  request(0, "shop.example.com", "document"),
  request(1, "cdn.example.com", "image"),
  request(2, "cdn.example.com", "image"),
  request(3, "cdn.example.com", "script"),
  request(4, "shop.example.com", "api"),
  request(5, "analytics.test", "api"),
  request(6, "shop.example.com", "api"),
  request(7, "shop.example.com", "image"),
];

test("hosts are listed with the busiest API first, not the busiest overall", () => {
  assert.deepEqual(
    hostCounts(recording).map((entry) => [entry.host, entry.api, entry.total]),
    [
      ["shop.example.com", 2, 4],
      ["analytics.test", 1, 1],
      ["cdn.example.com", 0, 3],
    ],
  );
  assert.equal(defaultHost(recording), "shop.example.com");
  assert.equal(defaultHost([]), "");
});

test("API calls are ticked by default, and everything is when there are none", () => {
  assert.deepEqual([...defaultKinds(recording, "shop.example.com")], ["api"]);
  assert.deepEqual([...defaultKinds(recording, "cdn.example.com")].sort(), ["image", "script"]);
});

test("the requests shown are those of one host and of the ticked kinds, in the order recorded", () => {
  assert.deepEqual(
    visibleRequests(recording, "shop.example.com", new Set(["api"])).map((r) => r.position),
    [4, 6],
  );
  assert.deepEqual(
    visibleRequests(recording, "shop.example.com", new Set(["api", "image"])).map((r) => r.position),
    [4, 6, 7],
  );
  assert.deepEqual(visibleRequests(recording, "shop.example.com", new Set()), []);
});

test("kinds are counted per host", () => {
  assert.equal(kindCounts(recording, "cdn.example.com").get("image"), 2);
  assert.equal(kindCounts(recording, "cdn.example.com").get("api"), undefined);
});

test("five hundred requests are sorted and filtered in a few milliseconds", () => {
  const many = Array.from({ length: 500 }, (_, index) =>
    request(index, index % 5 === 0 ? "cdn.example.com" : "shop.example.com", index % 3 === 0 ? "image" : "api"),
  );

  const started = performance.now();
  const host = defaultHost(many);
  const shown = visibleRequests(many, host, defaultKinds(many, host));
  const elapsed = performance.now() - started;

  assert.equal(host, "shop.example.com");
  assert.ok(shown.length > 0);
  assert.ok(elapsed < 50, `took ${elapsed} ms`);
});
