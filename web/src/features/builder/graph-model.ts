import type { GraphEdge, GraphNode, ScenarioGraph } from "../scenarios/types.ts";
import { exits, hasOneExit } from "./kinds.ts";

export type Position = { x: number; y: number };

export const columnWidth = 270;
export const rowHeight = 120;
// How many columns a band holds before the layout starts a new one below.
export const columnsPerBand = 6;

// clean drops what a node leaves empty, so that the same node always has the same
// shape: a graph the server sent and one the editor rebuilt must compare equal
// when nothing was changed, or every page would open as having unsaved changes.
export function clean(node: GraphNode): GraphNode {
  const out: Record<string, unknown> = {};
  for (const [key, value] of Object.entries(node)) {
    if (value === undefined || value === "" || value === false || value === 0) continue;
    if (Array.isArray(value) && value.length === 0) continue;
    if (key === "headers" && Object.keys(value as object).length === 0) continue;
    out[key] = value;
  }
  return out as GraphNode;
}

// canonical is what two graphs are compared by.
export function canonical(graph: ScenarioGraph): string {
  return JSON.stringify({
    start: graph.start,
    nodes: graph.nodes.map(clean),
    edges: graph.edges.map((edge) => (edge.when ? edge : { from: edge.from, to: edge.to })),
  });
}

// layout places each node one column right of the first node that leads to it, and
// stacks the nodes of a column. The graph holds no positions — the model has no
// field for one — so the same graph always opens laid out the same way.
export function layout(graph: ScenarioGraph): Record<string, Position> {
  const next = new Map<string, string[]>();
  for (const edge of graph.edges) next.set(edge.from, [...(next.get(edge.from) ?? []), edge.to]);

  const depth = new Map<string, number>();
  const queue: string[] = [];
  if (graph.nodes.some((node) => node.id === graph.start)) {
    depth.set(graph.start, 0);
    queue.push(graph.start);
  }
  while (queue.length > 0) {
    const current = queue.shift() as string;
    for (const to of next.get(current) ?? []) {
      if (!depth.has(to)) {
        depth.set(to, (depth.get(current) as number) + 1);
        queue.push(to);
      }
    }
  }

  // What the start does not reach (a node being wired) goes in a column after the
  // rest, so it is visible instead of hidden under another.
  const last = Math.max(-1, ...depth.values());
  for (const node of graph.nodes) if (!depth.has(node.id)) depth.set(node.id, last + 1);

  // A long chain wraps into bands of a few columns, like text, so that fifty
  // steps are a page and not a corridor.
  const rows = new Map<number, number>();
  const rowOf: Record<string, number> = {};
  for (const node of graph.nodes) {
    const column = depth.get(node.id) as number;
    const row = rows.get(column) ?? 0;
    rows.set(column, row + 1);
    rowOf[node.id] = row;
  }
  const tallest = Math.max(1, ...rows.values());

  const positions: Record<string, Position> = {};
  for (const node of graph.nodes) {
    const column = depth.get(node.id) as number;
    const band = Math.floor(column / columnsPerBand);
    positions[node.id] = {
      x: (column % columnsPerBand) * columnWidth,
      y: band * (tallest + 1) * rowHeight + rowOf[node.id] * rowHeight,
    };
  }
  return positions;
}

// nextId is an id no node uses: n1, n2…
export function nextId(nodes: { id: string }[]): string {
  const taken = new Set(nodes.map((node) => node.id));
  let n = nodes.length + 1;
  while (taken.has(`n${n}`)) n++;
  return `n${n}`;
}

// What is wrong with a node in the graph, in words, by node id. The server is the
// judge of a graph; this is what the canvas can say on its own, so that a node
// that needs a wire says so before anybody presses Validate.
export function issues(graph: ScenarioGraph): Record<string, string> {
  const found: Record<string, string> = {};
  const out = new Map<string, GraphEdge[]>();
  for (const edge of graph.edges) out.set(edge.from, [...(out.get(edge.from) ?? []), edge]);

  for (const node of graph.nodes) {
    const edges = out.get(node.id) ?? [];
    if (node.kind === "end") continue;

    if (hasOneExit(node.kind) && edges.length !== 1) {
      found[node.id] = "Nothing follows this step";
      continue;
    }
    const missing = exits(node.kind).filter((when) => !edges.some((edge) => edge.when === when));
    if (missing.length > 0) found[node.id] = `Missing the ${missing.join(" and ")} way out`;
  }

  const reached = new Set<string>([graph.start]);
  const queue = [graph.start];
  while (queue.length > 0) {
    const current = queue.shift() as string;
    for (const edge of out.get(current) ?? []) {
      if (!reached.has(edge.to)) {
        reached.add(edge.to);
        queue.push(edge.to);
      }
    }
  }
  for (const node of graph.nodes) {
    if (!reached.has(node.id) && !found[node.id]) found[node.id] = "No path leads here from the start";
  }

  return found;
}

// startOf keeps the start the graph has while it still exists; otherwise the first
// node nothing leads to, since that is where a path begins.
export function startOf(nodes: GraphNode[], edges: GraphEdge[], current: string): string {
  if (nodes.some((node) => node.id === current)) return current;
  const targets = new Set(edges.map((edge) => edge.to));
  return (nodes.find((node) => !targets.has(node.id)) ?? nodes[0])?.id ?? "";
}

// edgeId names an edge by what it joins, which is all that tells two edges apart.
export function edgeId(edge: GraphEdge): string {
  return `${edge.from}|${edge.to}|${edge.when ?? ""}`;
}

// freeExit is the next way out of a node that has no wire yet: what a new node
// should be attached to. Null when every exit is used, or the node has none.
export function freeExit(node: GraphNode, edges: GraphEdge[]): { when?: GraphEdge["when"] } | null {
  const used = edges.filter((edge) => edge.from === node.id);

  if (hasOneExit(node.kind)) return used.length === 0 ? {} : null;
  const when = exits(node.kind).find((way) => !used.some((edge) => edge.when === way));
  return when ? { when } : null;
}

// without removes nodes, and every edge that touched them.
export function without(graph: ScenarioGraph, removed: ReadonlySet<string>): ScenarioGraph {
  const nodes = graph.nodes.filter((node) => !removed.has(node.id));
  const edges = graph.edges.filter((edge) => !removed.has(edge.from) && !removed.has(edge.to));
  return { start: startOf(nodes, edges, graph.start), nodes, edges };
}

// connect wires one exit to a node. An exit leads to one place only, so a wire
// already leaving the same exit is replaced rather than joined by a second.
export function connect(graph: ScenarioGraph, edge: GraphEdge): ScenarioGraph {
  const edges = graph.edges.filter((e) => !(e.from === edge.from && (e.when ?? "") === (edge.when ?? "")));
  return { ...graph, edges: [...edges, edge] };
}
