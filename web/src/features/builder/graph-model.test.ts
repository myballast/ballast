import assert from "node:assert/strict";
import { test } from "node:test";

import type { ScenarioGraph } from "../scenarios/types.ts";
import { canonical, clean, issues, layout, nextId, startOf } from "./graph-model.ts";
import { describe, newNode } from "./kinds.ts";
import { emptyHistory, maxSteps, redo, remember, undo } from "./history.ts";

const graph: ScenarioGraph = {
  start: "n1",
  nodes: [
    { id: "n1", kind: "http_request", method: "POST", path: "/login", headers: { Accept: "application/json" } },
    { id: "n2", kind: "condition", expression: "status == 200" },
    { id: "n3", kind: "end" },
    { id: "n4", kind: "end" },
  ],
  edges: [
    { from: "n1", to: "n2" },
    { from: "n2", to: "n3", when: "true" },
    { from: "n2", to: "n4", when: "false" },
  ],
};

test("a sound graph has nothing to say", () => {
  assert.deepEqual(issues(graph), {});
});

test("empty fields do not make a graph differ from itself", () => {
  const rebuilt = { ...graph, nodes: graph.nodes.map((n) => ({ ...n, body: "", query: [], critical: false })) };
  assert.equal(canonical(rebuilt), canonical(graph));
  assert.deepEqual(clean({ id: "a", kind: "end", label: "" }), { id: "a", kind: "end" });
});

test("a changed field does", () => {
  const changed = structuredClone(graph);
  changed.nodes[0].path = "/signin";
  assert.notEqual(canonical(changed), canonical(graph));
});

test("the layout is the same every time and gives each node its own place", () => {
  const first = layout(graph);
  assert.deepEqual(layout(graph), first);

  const places = new Set(Object.values(first).map((p) => `${p.x},${p.y}`));
  assert.equal(places.size, graph.nodes.length);
  // The two ends are both one column after the condition, one above the other.
  assert.equal(first.n3.x, first.n4.x);
  assert.notEqual(first.n3.y, first.n4.y);
  assert.ok(first.n2.x > first.n1.x);
});

test("a node nothing leads to is still given a place", () => {
  const loose = { ...graph, nodes: [...graph.nodes, newNode("end", "n5")] };
  const places = layout(loose);
  assert.ok(places.n5.x > places.n3.x);
});

test("a step with nothing after it, a branch missing and an orphan are each named", () => {
  const broken: ScenarioGraph = {
    start: "n1",
    nodes: [
      { id: "n1", kind: "http_request", method: "GET", path: "/" },
      { id: "n2", kind: "condition", expression: "status == 200" },
      newNode("end", "n3"),
      newNode("think_time", "n4"),
    ],
    edges: [
      { from: "n1", to: "n2" },
      { from: "n2", to: "n3", when: "true" },
    ],
  };
  const found = issues(broken);
  assert.match(found.n2, /false/);
  assert.match(found.n4, /Nothing follows/);
  assert.equal(found.n1, undefined);
});

test("ids never repeat", () => {
  assert.equal(nextId(graph.nodes), "n5");
  assert.equal(nextId([{ id: "n2" }, { id: "n1" }]), "n3");
  assert.equal(nextId([{ id: "n3" }]), "n2");
});

test("the start is kept while it exists, else the first node nothing leads to", () => {
  assert.equal(startOf(graph.nodes, graph.edges, "n1"), "n1");
  assert.equal(startOf(graph.nodes.slice(1), graph.edges.slice(1), "n1"), "n2");
  assert.equal(startOf([], [], "n1"), "");
});

test("a secret is named on the canvas and never shown as a value", () => {
  const node = { id: "a", kind: "set_variable" as const, variable: "token", from_env: "API_TOKEN" };
  assert.equal(describe(node).sub, "secret API_TOKEN");
});

test("history: undo and redo walk the steps, and a new change ends the future", () => {
  let history = emptyHistory<number>();
  history = remember(history, 1);
  history = remember(history, 2);

  const back = undo(history, 3);
  assert.equal(back?.value, 2);
  const again = undo(back!.history, 2);
  assert.equal(again?.value, 1);
  assert.equal(undo(again!.history, 1), null);

  const forward = redo(again!.history, 1);
  assert.equal(forward?.value, 2);

  const replaced = remember(back!.history, 2);
  assert.equal(redo(replaced, 9), null);
});

test("history is bounded", () => {
  let history = emptyHistory<number>();
  for (let i = 0; i < maxSteps + 20; i++) history = remember(history, i);
  assert.equal(history.past.length, maxSteps);
  assert.equal(history.past[0], 20);
});

import { connect, edgeId, freeExit, without } from "./graph-model.ts";

test("a new node is attached to the first exit that leads nowhere", () => {
  const open: ScenarioGraph = { ...graph, edges: [{ from: "n1", to: "n2" }, { from: "n2", to: "n3", when: "true" }] };
  assert.deepEqual(freeExit(open.nodes[1], open.edges), { when: "false" });
  assert.equal(freeExit(open.nodes[0], open.edges), null);
  assert.deepEqual(freeExit(open.nodes[0], []), {});
  assert.equal(freeExit(open.nodes[2], open.edges), null);
});

test("removing a node removes its wires and moves the start on", () => {
  const rest = without(graph, new Set(["n1"]));
  assert.deepEqual(rest.nodes.map((n) => n.id), ["n2", "n3", "n4"]);
  assert.equal(rest.edges.length, 2);
  assert.equal(rest.start, "n2");
  assert.equal(without(graph, new Set(["n3"])).edges.some((e) => e.to === "n3"), false);
});

test("an exit leads to one place: connecting it again replaces the wire", () => {
  const moved = connect(graph, { from: "n2", to: "n4", when: "true" });
  const leaving = moved.edges.filter((e) => e.from === "n2");
  assert.equal(leaving.length, 2);
  assert.equal(leaving.find((e) => e.when === "true")?.to, "n4");
  assert.equal(edgeId({ from: "a", to: "b" }), "a|b|");
});

test("a long chain wraps into bands and every node keeps its own place", () => {
  const nodes = Array.from({ length: 50 }, (_, i) => ({ id: `n${i + 1}`, kind: "end" as const }));
  const chain: ScenarioGraph = {
    start: "n1",
    nodes,
    edges: nodes.slice(1).map((n, i) => ({ from: `n${i + 1}`, to: n.id })),
  };
  const places = layout(chain);

  assert.equal(new Set(Object.values(places).map((p) => `${p.x},${p.y}`)).size, 50);
  const width = Math.max(...Object.values(places).map((p) => p.x));
  assert.ok(width <= 5 * 270, `the layout is ${width} wide`);
  // A step is always right of or below the one before it.
  assert.ok(places.n7.y > places.n6.y);
});
