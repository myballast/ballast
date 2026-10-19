import assert from "node:assert/strict";
import { test } from "node:test";

import { arrange, infrastructuresOf, parseStack, place, subtitle } from "./arrangement.ts";
import type { ProjectOverview } from "./types.ts";

type Run = { status: string; at: string };

const overview = (name: string, extra: { favorite?: boolean; infra?: string; run?: Run; created?: string } = {}): ProjectOverview =>
  ({
    project: {
      id: name,
      name,
      slug: name,
      infra: extra.infra ?? "",
      region: "",
      owner: "",
      stack: [],
      favorite: extra.favorite ?? false,
      created_at: extra.created ?? "2026-01-01T00:00:00Z",
    },
    access: "full",
    environments: [],
    lastRun: extra.run ? { id: name, status: extra.run.status, created_at: extra.run.at, ended_at: extra.run.at } : null,
  }) as unknown as ProjectOverview;

const names = (list: ProjectOverview[]) => list.map((o) => o.project.name);

test("the default order is the most recent activity first", () => {
  const list = [
    overview("old", { run: { status: "passed", at: "2026-10-01T00:00:00Z" } }),
    overview("new", { run: { status: "passed", at: "2026-10-12T00:00:00Z" } }),
    overview("never", { created: "2026-10-05T00:00:00Z" }),
  ];
  assert.deepEqual(names(arrange(list, "recent")), ["new", "never", "old"]);
});

test("favourites come first, whatever the order is", () => {
  const list = [
    overview("a", { run: { status: "failed", at: "2026-10-12T00:00:00Z" } }),
    overview("b", { favorite: true, run: { status: "passed", at: "2026-01-02T00:00:00Z" } }),
    overview("c", { favorite: true, run: { status: "passed", at: "2026-10-01T00:00:00Z" } }),
  ];
  for (const sort of ["recent", "name", "status"] as const) {
    const ordered = names(arrange(list, sort));
    assert.deepEqual(ordered.slice(0, 2).sort(), ["b", "c"], `${sort}: ${ordered}`);
    assert.equal(ordered[2], "a");
  }
  // And within the favourites the chosen order holds.
  assert.deepEqual(names(arrange(list, "recent")), ["c", "b", "a"]);
  assert.deepEqual(names(arrange(list, "name")), ["b", "c", "a"]);
});

test("by name, without regard to case", () => {
  const list = [overview("zeta"), overview("Alpha"), overview("beta")];
  assert.deepEqual(names(arrange(list, "name")), ["Alpha", "beta", "zeta"]);
});

test("by last status, what asks for attention comes first and never-run last", () => {
  const at = "2026-10-01T00:00:00Z";
  const list = [
    overview("never"),
    overview("passed", { run: { status: "passed", at } }),
    overview("running", { run: { status: "running", at } }),
    overview("failed", { run: { status: "failed", at } }),
    overview("errored", { run: { status: "errored", at } }),
    overview("cancelled", { run: { status: "cancelled", at } }),
  ];
  assert.deepEqual(names(arrange(list, "status")), ["failed", "errored", "running", "passed", "cancelled", "never"]);
});

test("two with the same status are ordered by activity, then name", () => {
  const list = [
    overview("b", { run: { status: "failed", at: "2026-10-01T00:00:00Z" } }),
    overview("a", { run: { status: "failed", at: "2026-10-01T00:00:00Z" } }),
    overview("c", { run: { status: "failed", at: "2026-10-09T00:00:00Z" } }),
  ];
  assert.deepEqual(names(arrange(list, "status")), ["c", "a", "b"]);
});

test("the list that was passed is not reordered", () => {
  const list = [overview("b"), overview("a")];
  arrange(list, "name");
  assert.deepEqual(names(list), ["b", "a"]);
});

test("the subtitle counts distinct infrastructures, and leaves out what nobody said", () => {
  const list = [
    overview("a", { infra: "k8s" }),
    overview("b", { infra: "K8S" }),
    overview("c", { infra: "railway" }),
    overview("d"),
  ];
  assert.equal(infrastructuresOf(list), 2);
  assert.equal(subtitle(list), "4 projects across 2 infrastructures");
});

test("the subtitle in the singular, and with no infrastructure at all", () => {
  assert.equal(subtitle([overview("a", { infra: "vm" })]), "1 project across 1 infrastructure");
  assert.equal(subtitle([overview("a"), overview("b")]), "2 projects");
  assert.equal(subtitle([]), "0 projects");
});

test("a place says what is known and never undefined or a stray separator", () => {
  assert.equal(place({ infra: "k8s", region: "eu-west-1" }), "k8s · eu-west-1");
  assert.equal(place({ infra: "railway", region: "" }), "railway");
  assert.equal(place({ infra: "", region: "eu-west-1" }), "eu-west-1");
  assert.equal(place({ infra: " ", region: "" }), "");
});

test("a stack is typed with commas or spaces", () => {
  assert.deepEqual(parseStack("go, pg  k6,,rds "), ["go", "pg", "k6", "rds"]);
  assert.deepEqual(parseStack(""), []);
  assert.deepEqual(parseStack(" , "), []);
});
