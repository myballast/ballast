import assert from "node:assert/strict";
import test from "node:test";

import { copyFlow, maxNodes, parseClip, pasteFlow, serializeClip } from "./flow-clipboard.ts";
import type { GraphNode, ScenarioGraph } from "../scenarios/types.ts";

const graph: ScenarioGraph = {
  start: "n1",
  nodes: [
    { id: "n1", kind: "http_request", method: "POST", path: "/login", body: '{"a":1}', headers: { "Content-Type": "application/json" } },
    { id: "n2", kind: "extract_variable", variable: "token", source: "body", from: "token" },
    { id: "n3", kind: "condition", expression: "token != \"\"" },
    { id: "n4", kind: "end" },
  ],
  edges: [
    { from: "n1", to: "n2" },
    { from: "n2", to: "n3" },
    { from: "n3", to: "n4", when: "true" },
  ],
};
const places = { n1: { x: 100, y: 50 }, n2: { x: 370, y: 50 }, n3: { x: 640, y: 50 }, n4: { x: 910, y: 50 } };

test("a copy holds the chosen steps and the wires between them only", () => {
  const clip = copyFlow(graph, new Set(["n1", "n2"]), places);
  assert.ok(clip);
  assert.deepEqual(clip.nodes.map((n) => n.id), ["n1", "n2"]);
  assert.deepEqual(clip.edges, [{ from: "n1", to: "n2" }]);
  assert.deepEqual(clip.places, { n1: { x: 0, y: 0 }, n2: { x: 270, y: 0 } });
  assert.equal(copyFlow(graph, new Set(), places), null);
});

test("editing the original after a copy does not reach the copy", () => {
  const clip = copyFlow(graph, new Set(["n1"]), places);
  assert.ok(clip);
  (graph.nodes[0] as GraphNode).path = "/changed";
  assert.equal(clip.nodes[0].path, "/login");
  (graph.nodes[0] as GraphNode).path = "/login";
});

test("pasting gives new ids, remaps the wires and places the group", () => {
  const clip = copyFlow(graph, new Set(["n2", "n3"]), places);
  assert.ok(clip);
  const pasted = pasteFlow(graph, clip, { x: 1000, y: 200 });
  assert.ok(pasted.ok);
  assert.deepEqual(pasted.ids, ["n5", "n6"]);
  assert.equal(pasted.graph.nodes.length, 6);
  assert.deepEqual(pasted.graph.edges.at(-1), { from: "n5", to: "n6" });
  assert.equal(pasted.graph.start, "n1");
  assert.deepEqual(pasted.places, { n5: { x: 1000, y: 200 }, n6: { x: 1270, y: 200 } });
  // The original is untouched.
  assert.equal(graph.nodes.length, 4);
});

test("a pasted flow into an empty scenario becomes its start", () => {
  const clip = copyFlow(graph, new Set(["n1", "n2", "n3", "n4"]), places);
  assert.ok(clip);
  const pasted = pasteFlow({ start: "", nodes: [], edges: [] }, clip, { x: 0, y: 0 });
  assert.ok(pasted.ok);
  assert.equal(pasted.graph.start, "n1");
  assert.equal(pasted.graph.edges.length, 3);
});

test("a labelled exit keeps its label", () => {
  const clip = copyFlow(graph, new Set(["n3", "n4"]), places);
  assert.ok(clip);
  const pasted = pasteFlow(graph, clip, { x: 0, y: 0 });
  assert.ok(pasted.ok);
  assert.deepEqual(pasted.graph.edges.at(-1), { from: "n5", to: "n6", when: "true" });
});

test("a scenario never grows past what the API accepts", () => {
  const big: ScenarioGraph = {
    start: "n1",
    nodes: Array.from({ length: maxNodes - 1 }, (_, i) => ({ id: `n${i + 1}`, kind: "end" as const })),
    edges: [],
  };
  const clip = copyFlow(graph, new Set(["n1", "n2"]), places);
  assert.ok(clip);
  const pasted = pasteFlow(big, clip, { x: 0, y: 0 });
  assert.equal(pasted.ok, false);
});

test("a copy survives the clipboard", () => {
  const clip = copyFlow(graph, new Set(["n1", "n2", "n3"]), places);
  assert.ok(clip);
  const read = parseClip(serializeClip(clip));
  assert.ok(read.ok);
  assert.deepEqual(read.clip, clip);
  assert.equal(read.dropped, 0);
});

test("text that is not a copied flow is not one", () => {
  for (const text of ["", "hello", "[1,2]", "null", "{}", '{"ballast":"other"}', "{not json"]) {
    const read = parseClip(text);
    assert.equal(read.ok, false, text);
  }
});

test("only a closed set of kinds and known fields come through", () => {
  const hostile = JSON.stringify({
    ballast: "flow",
    version: 1,
    nodes: [
      { id: "a", kind: "http_request", method: "GET", path: "/x", script: "evil()", __proto__: { polluted: true }, constructor: "x", critical: "yes" },
      { id: "b", kind: "run_shell", command: "rm -rf /" },
    ],
    edges: [],
  });
  assert.equal(parseClip(hostile).ok, false);

  const kept = parseClip(
    JSON.stringify({
      ballast: "flow",
      version: 1,
      nodes: [{ id: "a", kind: "http_request", method: "GET", path: "/x", script: "evil()", critical: "yes", iterations: "many", seconds: Infinity }],
      edges: [{ from: "a", to: "ghost" }, { from: "a", to: "a", when: "sideways" }],
    }),
  );
  assert.ok(kept.ok);
  assert.deepEqual(kept.clip.nodes, [{ id: "a", kind: "http_request", method: "GET", path: "/x" }]);
  assert.deepEqual(kept.clip.edges, []);
});

test("a credential header with no variable is left out, and counted", () => {
  const read = parseClip(
    JSON.stringify({
      ballast: "flow",
      version: 1,
      nodes: [
        {
          id: "a",
          kind: "http_request",
          method: "GET",
          path: "/me",
          headers: { Authorization: "Bearer eyJ0eXAi", Cookie: "session=abc", "X-Id": "7", "Proxy-Authorization": "Bearer {{token}}" },
        },
      ],
      edges: [],
    }),
  );
  assert.ok(read.ok);
  assert.deepEqual(read.clip.nodes[0].headers, { "X-Id": "7", "Proxy-Authorization": "Bearer {{token}}" });
  assert.equal(read.dropped, 2);
});

test("too many steps, the same id twice, and huge text are refused", () => {
  const many = JSON.stringify({ ballast: "flow", version: 1, nodes: Array.from({ length: 51 }, (_, i) => ({ id: `n${i}`, kind: "end" })), edges: [] });
  assert.equal(parseClip(many).ok, false);

  const twice = JSON.stringify({ ballast: "flow", version: 1, nodes: [{ id: "a", kind: "end" }, { id: "a", kind: "end" }], edges: [] });
  assert.equal(parseClip(twice).ok, false);

  assert.equal(parseClip("x".repeat(300_000)).ok, false);
  assert.equal(parseClip(JSON.stringify({ ballast: "flow", version: 2, nodes: [], edges: [] })).ok, false);
});

test("two wires out of one exit keep the first", () => {
  const read = parseClip(
    JSON.stringify({
      ballast: "flow",
      version: 1,
      nodes: [{ id: "a", kind: "end" }, { id: "b", kind: "end" }, { id: "c", kind: "end" }],
      edges: [{ from: "a", to: "b" }, { from: "a", to: "c" }],
    }),
  );
  assert.ok(read.ok);
  assert.deepEqual(read.clip.edges, [{ from: "a", to: "b" }]);
});
