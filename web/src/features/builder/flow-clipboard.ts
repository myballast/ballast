import { nodeKinds, type GraphEdge, type GraphNode, type ScenarioGraph } from "../scenarios/types.ts";
import { nextId, startOf, type Position } from "./graph-model.ts";
import { isCredentialHeader, readsAVariable } from "./request-validation.ts";

// Copying and pasting a flow: some steps and the wires between them, to the same
// scenario or to another one.
//
// What comes in through the clipboard is somebody else's text, whatever put it
// there. It is read as data: only the closed set of kinds, only the fields a node
// has, only values of the right type and a sane size. The result then goes through
// every check a graph goes through — the API judges it again when it is saved.

const marker = "flow";

// What one copy may hold, and what a graph may hold in all (the API's own limit).
export const maxCopied = 50;
export const maxNodes = 200;
const maxText = 200_000;
const maxString = 5_000;

export type Clip = {
  nodes: GraphNode[];
  edges: GraphEdge[];
  // Where each step was, from the top left of the group.
  places: Record<string, Position>;
};

// copyFlow takes the chosen steps, and the wires that join two of them. A wire to a
// step that was not chosen is left out: pasted, it would lead nowhere.
export function copyFlow(graph: ScenarioGraph, ids: ReadonlySet<string>, places: Record<string, Position>): Clip | null {
  const nodes = graph.nodes.filter((n) => ids.has(n.id));
  if (nodes.length === 0) return null;

  const left = Math.min(...nodes.map((n) => places[n.id]?.x ?? 0));
  const top = Math.min(...nodes.map((n) => places[n.id]?.y ?? 0));

  return {
    // A copy, so that editing the original after cannot reach the clip.
    nodes: JSON.parse(JSON.stringify(nodes)) as GraphNode[],
    edges: graph.edges.filter((e) => ids.has(e.from) && ids.has(e.to)).map((e) => ({ ...e })),
    places: Object.fromEntries(nodes.map((n) => [n.id, { x: (places[n.id]?.x ?? 0) - left, y: (places[n.id]?.y ?? 0) - top }])),
  };
}

export function serializeClip(clip: Clip): string {
  return JSON.stringify({ ballast: marker, version: 1, ...clip });
}

export type Parsed = { ok: true; clip: Clip; dropped: number } | { ok: false; error: string };

const refusal = (error: string): Parsed => ({ ok: false, error });

const text = (value: unknown): string | undefined => (typeof value === "string" && value.length <= maxString ? value : undefined);
const number = (value: unknown): number | undefined => (typeof value === "number" && Number.isFinite(value) ? value : undefined);
const record = (value: unknown): Record<string, unknown> | null =>
  typeof value === "object" && value !== null && !Array.isArray(value) ? (value as Record<string, unknown>) : null;

// parseClip reads the text on the clipboard. Text that is not a copied flow is not
// an error to show: pasting is also what a person does with something else.
export function parseClip(raw: string): Parsed {
  if (raw.length === 0 || raw.length > maxText) return refusal("");

  let body: Record<string, unknown> | null;
  try {
    body = record(JSON.parse(raw));
  } catch {
    return refusal("");
  }
  if (!body || body.ballast !== marker) return refusal("");
  if (body.version !== 1) return refusal("This copy comes from a version of Ballast this one does not read.");

  if (!Array.isArray(body.nodes) || body.nodes.length === 0) return refusal("The copy holds no step.");
  if (body.nodes.length > maxCopied) return refusal(`A copy holds at most ${maxCopied} steps.`);

  let dropped = 0;
  const nodes: GraphNode[] = [];
  const seen = new Set<string>();
  for (const item of body.nodes) {
    const parsed = cleanNode(item);
    if (!parsed) return refusal("The copy holds something that is not a step.");
    if (seen.has(parsed.node.id)) return refusal("The copy holds two steps with the same id.");
    seen.add(parsed.node.id);
    dropped += parsed.dropped;
    nodes.push(parsed.node);
  }

  const edges: GraphEdge[] = [];
  const wired = new Set<string>();
  for (const item of Array.isArray(body.edges) ? body.edges.slice(0, maxCopied * 2) : []) {
    const e = record(item);
    const from = text(e?.from);
    const to = text(e?.to);
    if (!from || !to || !seen.has(from) || !seen.has(to)) continue;
    const when = e?.when;
    if (when !== undefined && when !== "true" && when !== "false" && when !== "body" && when !== "after") continue;
    // One wire per exit, as a graph holds.
    const key = `${from}|${when ?? ""}`;
    if (wired.has(key)) continue;
    wired.add(key);
    edges.push(when ? { from, to, when } : { from, to });
  }

  const given = record(body.places) ?? {};
  const places: Record<string, Position> = {};
  for (const node of nodes) {
    const at = record(given[node.id]);
    places[node.id] = { x: number(at?.x) ?? 0, y: number(at?.y) ?? 0 };
  }

  return { ok: true, clip: { nodes, edges, places }, dropped };
}

// cleanNode keeps what a node of the graph may hold, and nothing else. A credential
// header with no variable in it is not kept: a node reads a credential from a
// variable, never carries one.
function cleanNode(item: unknown): { node: GraphNode; dropped: number } | null {
  const raw = record(item);
  if (!raw) return null;

  const id = text(raw.id);
  const kind = nodeKinds.find((k) => k === raw.kind);
  if (!id || id.length > 64 || !kind) return null;

  const node: GraphNode = { id, kind };
  let dropped = 0;

  for (const field of ["method", "path", "body", "variable", "from", "value", "from_env", "expression", "over", "item", "group", "label"] as const) {
    const value = text(raw[field]);
    if (value !== undefined) node[field] = value;
  }
  for (const field of ["iterations", "seconds", "seconds_max"] as const) {
    const value = number(raw[field]);
    if (value !== undefined) node[field] = value;
  }
  if (raw.source === "body" || raw.source === "header" || raw.source === "status") node.source = raw.source;
  if (typeof raw.critical === "boolean") node.critical = raw.critical;

  const headers = record(raw.headers);
  if (headers) {
    const kept: Record<string, string> = {};
    for (const [name, value] of Object.entries(headers).slice(0, 50)) {
      const v = text(value);
      if (v === undefined || name.length > 200) continue;
      if (isCredentialHeader(name) && !readsAVariable(v)) {
        dropped++;
        continue;
      }
      kept[name] = v;
    }
    if (Object.keys(kept).length > 0) node.headers = kept;
  }

  if (Array.isArray(raw.query)) {
    const query = raw.query.slice(0, 50).flatMap((q) => {
      const row = record(q);
      const name = text(row?.name);
      const value = text(row?.value);
      return name !== undefined && value !== undefined ? [{ name, value }] : [];
    });
    if (query.length > 0) node.query = query;
  }

  return { node, dropped };
}

export type Pasted =
  | { ok: true; graph: ScenarioGraph; ids: string[]; places: Record<string, Position> }
  | { ok: false; error: string };

// pasteFlow adds a copy of the clip to the graph: new ids, the wires between the
// pasted steps remapped to them, the group placed with its top left at `at`. The
// graph keeps its start; an empty graph takes the pasted flow's.
export function pasteFlow(graph: ScenarioGraph, clip: Clip, at: Position): Pasted {
  if (graph.nodes.length + clip.nodes.length > maxNodes) {
    return { ok: false, error: `A scenario holds at most ${maxNodes} steps: there is no room for ${clip.nodes.length} more.` };
  }

  const renamed = new Map<string, string>();
  const nodes = [...graph.nodes];
  const added: GraphNode[] = [];
  for (const node of clip.nodes) {
    const id = nextId(nodes);
    renamed.set(node.id, id);
    const copy = { ...node, id };
    nodes.push(copy);
    added.push(copy);
  }

  const edges = clip.edges.flatMap((e) => {
    const from = renamed.get(e.from);
    const to = renamed.get(e.to);
    return from && to ? [{ ...e, from, to }] : [];
  });

  const places = Object.fromEntries(
    clip.nodes.map((n) => [renamed.get(n.id) as string, { x: at.x + (clip.places[n.id]?.x ?? 0), y: at.y + (clip.places[n.id]?.y ?? 0) }]),
  );

  const next: ScenarioGraph = { ...graph, nodes, edges: [...graph.edges, ...edges] };
  if (graph.nodes.length === 0) next.start = startOf(added, edges, "");
  return { ok: true, graph: next, ids: added.map((n) => n.id), places };
}
