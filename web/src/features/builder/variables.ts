import type { GraphEdge, GraphNode, ScenarioGraph } from "../scenarios/types.ts";

export type Available = { name: string; secret: boolean; from: string };

// variablesBefore lists what a node can read: the variables written by the nodes
// that lead to it, nearest first. Walking the graph backwards is enough; a loop
// that leads back to itself adds nothing new.
//
// A variable bound to a secret is listed with its name and a flag, and nothing
// else: the graph holds the secret's key, never its value, so there is no value to
// show.
export function variablesBefore(graph: ScenarioGraph, nodeId: string): Available[] {
  const into = new Map<string, GraphEdge[]>();
  for (const edge of graph.edges) into.set(edge.to, [...(into.get(edge.to) ?? []), edge]);
  const byId = new Map(graph.nodes.map((node) => [node.id, node]));

  const found: Available[] = [];
  const names = new Set<string>();
  const seen = new Set<string>([nodeId]);
  let frontier = [nodeId];

  while (frontier.length > 0) {
    const next: string[] = [];
    for (const id of frontier) {
      for (const edge of into.get(id) ?? []) {
        if (seen.has(edge.from)) continue;
        seen.add(edge.from);
        next.push(edge.from);

        const node = byId.get(edge.from);
        const written = node && variableWritten(node);
        if (written && !names.has(written.name)) {
          names.add(written.name);
          found.push(written);
        }
      }
    }
    frontier = next;
  }
  return found;
}

function variableWritten(node: GraphNode): Available | undefined {
  if (!node.variable) return undefined;
  if (node.kind === "extract_variable") return { name: node.variable, secret: false, from: node.id };
  if (node.kind === "set_variable") return { name: node.variable, secret: Boolean(node.from_env), from: node.id };
  return undefined;
}

// unboundSecrets are the keys of the environment's secrets that no node of the
// graph reads yet.
export function unboundSecrets(keys: string[], graph: ScenarioGraph): string[] {
  const bound = new Set(graph.nodes.map((node) => node.from_env).filter(Boolean));
  return keys.filter((key) => !bound.has(key));
}

// variableForKey turns a secret's key into a variable name nobody has taken:
// API_TOKEN becomes api_token, then api_token_2.
export function variableForKey(key: string, taken: Iterable<string>): string {
  const used = new Set(taken);
  let base = key.toLowerCase().replace(/[^a-z0-9_]/g, "_");
  if (!/^[a-z_]/.test(base)) base = `_${base}`;

  let name = base;
  for (let n = 2; used.has(name); n++) name = `${base}_${n}`;
  return name;
}

// bindSecretBefore adds a "Set variable" node that reads a secret, and puts it
// between a request and what led to it. Whatever pointed at the request now points
// at the new node, so the request runs after the secret is read; if the request
// was where the scenario began, the new node is.
export function bindSecretBefore(
  graph: ScenarioGraph,
  requestId: string,
  key: string,
  id: string,
): { graph: ScenarioGraph; variable: string } {
  const variable = variableForKey(
    key,
    graph.nodes.flatMap((node) => (node.variable ? [node.variable] : [])),
  );
  const node: GraphNode = { id, kind: "set_variable", variable, from_env: key };

  const edges = graph.edges.map((edge) => (edge.to === requestId ? { ...edge, to: id } : edge));
  edges.push({ from: id, to: requestId });

  return {
    variable,
    graph: {
      start: graph.start === requestId ? id : graph.start,
      nodes: [...graph.nodes, node],
      edges,
    },
  };
}

// undefinedNames: the variables a request reads that no node before it writes.
// Not an error — a variable can come from an earlier loop or a script — but
// something to say before a run reads "undefined" into an address.
export function undefinedNames(node: GraphNode, available: Available[]): string[] {
  const known = new Set(available.map((v) => v.name));
  const read = new Set<string>();

  for (const match of (node.path ?? "").matchAll(/\{([A-Za-z0-9_]*)\}/g)) read.add(match[1]);

  const texts = [
    node.body ?? "",
    ...Object.values(node.headers ?? {}),
    ...(node.query ?? []).map((q) => q.value),
  ];
  for (const text of texts) for (const match of text.matchAll(/\{\{([A-Za-z_][A-Za-z0-9_]*)\}\}/g)) read.add(match[1]);

  return [...read].filter((name) => name && !known.has(name));
}

// What typing {{ offers. The caret is inside an open {{ when the nearest one
// before it has no }} after it yet.
export type SuggestionContext = { from: number; query: string };

export function suggestionContext(text: string, caret: number): SuggestionContext | null {
  const before = text.slice(0, caret);
  const open = before.lastIndexOf("{{");
  if (open < 0) return null;

  const typed = before.slice(open + 2);
  if (typed.includes("}}") || /[^A-Za-z0-9_]/.test(typed)) return null;
  return { from: open, query: typed };
}

// applySuggestion writes {{name}} over what was typed of it, and says where the
// caret goes: after the closing braces. A }} already there, from a placeholder
// being edited, is taken over rather than doubled.
export function applySuggestion(
  text: string,
  caret: number,
  context: SuggestionContext,
  name: string,
): { text: string; caret: number } {
  const after = text.slice(caret);
  const rest = after.startsWith("}}") ? after.slice(2) : after;
  const inserted = `{{${name}}}`;
  return { text: text.slice(0, context.from) + inserted + rest, caret: context.from + inserted.length };
}
