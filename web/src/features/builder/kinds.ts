import type { GraphNode, NodeKind } from "../scenarios/types.ts";

// What the palette offers and what a new node starts as. The icon is named, not
// imported: this file is read by tests that load no component.
export type KindInfo = {
  kind: NodeKind;
  label: string;
  // One line for the palette's tooltip and the screen reader.
  hint: string;
  icon: "request" | "extract" | "set" | "condition" | "loop" | "wait" | "assert" | "end";
  // The accent of the kind, a token of globals.css. It is a help to the eye, never
  // what tells kinds apart: the label and the icon do.
  tone: Tone;
};

export type Tone = { text: string; tint: string; bar: string; var: string };

// Written out whole: Tailwind finds a class by reading it, not by building it.
const tones = {
  request: { text: "text-kind-request", tint: "bg-kind-request/15", bar: "bg-kind-request", var: "var(--kind-request)" },
  extract: { text: "text-kind-extract", tint: "bg-kind-extract/15", bar: "bg-kind-extract", var: "var(--kind-extract)" },
  set: { text: "text-kind-set", tint: "bg-kind-set/15", bar: "bg-kind-set", var: "var(--kind-set)" },
  condition: { text: "text-kind-condition", tint: "bg-kind-condition/15", bar: "bg-kind-condition", var: "var(--kind-condition)" },
  loop: { text: "text-kind-loop", tint: "bg-kind-loop/15", bar: "bg-kind-loop", var: "var(--kind-loop)" },
  wait: { text: "text-kind-wait", tint: "bg-kind-wait/15", bar: "bg-kind-wait", var: "var(--kind-wait)" },
  assert: { text: "text-kind-assert", tint: "bg-kind-assert/15", bar: "bg-kind-assert", var: "var(--kind-assert)" },
  end: { text: "text-kind-end", tint: "bg-kind-end/15", bar: "bg-kind-end", var: "var(--kind-end)" },
} satisfies Record<KindInfo["icon"], Tone>;

export const kindInfo: KindInfo[] = [
  { kind: "http_request", label: "HTTP request", hint: "Call an endpoint of the target", icon: "request", tone: tones.request },
  { kind: "extract_variable", label: "Extract variable", hint: "Keep a value from the last response", icon: "extract", tone: tones.extract },
  { kind: "set_variable", label: "Set variable", hint: "Give a variable a value or a secret", icon: "set", tone: tones.set },
  { kind: "condition", label: "Condition", hint: "Take one way or the other", icon: "condition", tone: tones.condition },
  { kind: "loop", label: "Loop", hint: "Repeat a few steps", icon: "loop", tone: tones.loop },
  { kind: "think_time", label: "Think time", hint: "Wait like a person would", icon: "wait", tone: tones.wait },
  { kind: "assertion", label: "Assertion", hint: "Check something and fail if it is false", icon: "assert", tone: tones.assert },
  { kind: "end", label: "End", hint: "Where a path of the scenario stops", icon: "end", tone: tones.end },
];

export function infoOf(kind: NodeKind): KindInfo {
  return kindInfo.find((info) => info.kind === kind) ?? kindInfo[0];
}

// The edge labels a kind sends out. A node that has none sends one plain edge,
// and an end sends none.
export function exits(kind: NodeKind): ("true" | "false" | "body" | "after")[] {
  if (kind === "condition") return ["true", "false"];
  if (kind === "loop") return ["body", "after"];
  return [];
}

export function hasOneExit(kind: NodeKind): boolean {
  return kind !== "end" && exits(kind).length === 0;
}

// A node a person has just added: valid on its own, so that adding one never
// makes a sound graph refusable by what it holds, only by where it is wired.
export function newNode(kind: NodeKind, id: string): GraphNode {
  switch (kind) {
    case "http_request":
      return { id, kind, method: "GET", path: "/" };
    case "extract_variable":
      return { id, kind, variable: "value", source: "body", from: "value" };
    case "set_variable":
      return { id, kind, variable: "value", value: "" };
    case "condition":
      return { id, kind, expression: "response_status == 200" };
    case "loop":
      return { id, kind, iterations: 3 };
    case "think_time":
      return { id, kind, seconds: 1 };
    case "assertion":
      return { id, kind, expression: "response_status == 200" };
    case "end":
      return { id, kind };
  }
}

// What a node says on the canvas: a title and a line under it, from what it holds.
export function describe(node: GraphNode): { title: string; sub: string } {
  const label = node.label?.trim();

  switch (node.kind) {
    case "http_request":
      return { title: label || `${node.method ?? "GET"} ${node.path ?? "/"}`, sub: label ? `${node.method} ${node.path}` : "" };
    case "extract_variable":
      return { title: label || `Extract ${node.variable ?? ""}`, sub: extractSource(node) };
    case "set_variable":
      return {
        title: label || `Set ${node.variable ?? ""}`,
        // The name of a secret, never what it holds: the graph does not hold it.
        sub: node.from_env ? `secret ${node.from_env}` : (node.value ?? ""),
      };
    case "condition":
      return { title: label || "Condition", sub: node.expression ?? "" };
    case "loop":
      return {
        title: label || "Loop",
        // The maximum is always on the node: it is the one number that bounds a loop.
        sub: node.over ? `each ${node.item ?? "item"} of ${node.over}, up to ${node.iterations ?? 0}` : `${node.iterations ?? 0} times`,
      };
    case "think_time":
      return {
        title: label || "Think time",
        sub: node.seconds_max ? `${node.seconds ?? 0}–${node.seconds_max} s` : `${node.seconds ?? 0} s`,
      };
    case "assertion":
      return { title: label || "Assertion", sub: node.expression ?? "" };
    case "end":
      return { title: label || "End", sub: "" };
  }
}

// Where an extraction reads, in a few words: "body · data.token", "header · X-Id",
// "status".
export function extractSource(node: GraphNode): string {
  const source = node.source ?? "body";
  if (source === "status") return "status";
  return `${source} · ${(node.from ?? "").replace(/^\$\./, "")}`;
}
