import type { ScenarioGraph, Threshold } from "./types.ts";

// What a threshold can say, for somebody who has never written one. A rule is a
// k6 metric and an expression; here it is built from a measure, a number and,
// optionally, one request, so that nobody needs to know k6's names. The domain
// still judges what is stored: this only writes the two strings.

export type MeasureId = "response_time" | "failed" | "checks" | "throughput";

type Stat = { id: string; label: string };

type Measure = {
  label: string;
  metric: string;
  stats: Stat[];
  // The comparison is fixed by the measure: a response time must stay under, a
  // share of checks that held must stay over.
  operator: "<" | ">";
  unit: string;
  // What a typed number is multiplied by to be k6's: 1 % is 0.01.
  scale: number;
  max?: number;
  start: number;
  // Whether a rule can be narrowed to one request: checks have no endpoint.
  byEndpoint: boolean;
};

export const measures: Record<MeasureId, Measure> = {
  response_time: {
    label: "Response time",
    metric: "http_req_duration",
    stats: [
      { id: "p(95)", label: "95 % of requests" },
      { id: "p(99)", label: "99 % of requests" },
      { id: "avg", label: "the average request" },
      { id: "med", label: "the typical request" },
      { id: "max", label: "the slowest request" },
    ],
    operator: "<",
    unit: "ms",
    scale: 1,
    start: 500,
    byEndpoint: true,
  },
  failed: {
    label: "Failed requests",
    metric: "http_req_failed",
    stats: [{ id: "rate", label: "share of requests" }],
    operator: "<",
    unit: "%",
    scale: 0.01,
    max: 100,
    start: 1,
    byEndpoint: true,
  },
  checks: {
    label: "Checks that hold",
    metric: "checks",
    stats: [{ id: "rate", label: "share of checks" }],
    operator: ">",
    unit: "%",
    scale: 0.01,
    max: 100,
    start: 99,
    byEndpoint: false,
  },
  throughput: {
    label: "Requests per second",
    metric: "http_reqs",
    stats: [{ id: "rate", label: "rate" }],
    operator: ">",
    unit: "req/s",
    scale: 1,
    start: 10,
    byEndpoint: true,
  },
};

export type RuleInput = {
  measure: MeasureId;
  stat: string;
  value: number;
  // "POST /login": the name tag of one request. Empty or absent means all of them.
  endpoint?: string;
};

export type BuiltRule =
  | { ok: true; metric: string; expression: string; sentence: string }
  | { ok: false; error: string };

// buildRule writes the k6 metric and expression for a rule, and says it in a sentence.
export function buildRule(input: RuleInput): BuiltRule {
  const measure = measures[input.measure];
  const stat = measure.stats.find((s) => s.id === input.stat);
  if (!stat) return { ok: false, error: "Pick what to measure." };

  const { value } = input;
  const ceiling = measure.max ?? Infinity;
  if (!Number.isFinite(value) || value <= 0 || value > ceiling) {
    return {
      ok: false,
      error: measure.max ? `Give a number above 0 and up to ${measure.max}.` : "Give a number above 0.",
    };
  }

  const endpoint = measure.byEndpoint ? (input.endpoint ?? "").trim() : "";
  // The tag is written between braces in k6's selector: a brace or a line break in
  // it would end the selector early.
  if (/[{}\r\n]/.test(endpoint)) {
    return { ok: false, error: "k6 cannot select a request whose path holds a variable. Leave it on all requests." };
  }

  const metric = endpoint ? `${measure.metric}{name:${endpoint}}` : measure.metric;
  const expression = `${stat.id}${measure.operator}${Number((value * measure.scale).toFixed(6))}`;
  const where = endpoint ? ` on ${endpoint}` : "";

  return { ok: true, metric, expression, sentence: sentenceOf(input.measure, stat, value, where) };
}

function sentenceOf(id: MeasureId, stat: Stat, value: number, where: string): string {
  const unit = measures[id].unit;
  switch (id) {
    case "response_time":
      return `${capital(stat.label)} must answer in under ${value} ${unit}${where}`;
    case "failed":
      return `Fewer than ${value} ${unit} of requests may fail${where}`;
    case "checks":
      return `At least ${value} ${unit} of the checks must hold`;
    case "throughput":
      return `The target must serve more than ${value} ${unit}${where}`;
  }
}

function capital(text: string): string {
  return text.charAt(0).toUpperCase() + text.slice(1);
}

// A preset is a rule most scenarios want, one click away. The numbers are starting
// points, common rules of thumb: the right threshold depends on the application.
export type Preset = {
  id: string;
  title: string;
  help: string;
  input: RuleInput;
};

export const presets: Preset[] = [
  {
    id: "fast",
    title: "Most requests are fast",
    help: "95 % of requests must answer in under 500 ms. The 95th percentile ignores a few slow outliers, so it shows what most users feel. Raise or lower 500 to match your application.",
    input: { measure: "response_time", stat: "p(95)", value: 500 },
  },
  {
    id: "reliable",
    title: "Almost no request fails",
    help: "Fewer than 1 % of requests may fail. Without this rule a run can read “Passed” while every request failed, because nothing was watching the errors.",
    input: { measure: "failed", stat: "rate", value: 1 },
  },
  {
    id: "checks",
    title: "Every check holds",
    help: "At least 99 % of the scenario's own checks must hold. It only means something if the scenario has checks, such as an assertion step.",
    input: { measure: "checks", stat: "rate", value: 99 },
  },
  {
    id: "outliers",
    title: "No slow outliers",
    help: "99 % of requests must answer in under 1500 ms. It catches the rare very slow request that the 95th percentile hides.",
    input: { measure: "response_time", stat: "p(99)", value: 1500 },
  },
];

// ruleOf is the metric and expression of a preset. A preset is always valid: the
// test builds every one of them.
export function ruleOf(preset: Preset): { metric: string; expression: string } {
  const built = buildRule(preset.input);
  if (!built.ok) throw new Error(`preset ${preset.id} does not build: ${built.error}`);
  return { metric: built.metric, expression: built.expression };
}

export function isAdded(thresholds: Threshold[], rule: { metric: string; expression: string }): boolean {
  return thresholds.some((t) => t.metric === rule.metric && t.expression === rule.expression);
}

export type EndpointChoice = { name: string; usable: boolean };

// endpointChoices lists the requests of a graph by the name tag the generator gives
// them: the method and the path template, once each. Only those: a rule on a
// request the scenario never sends would watch nothing.
export function endpointChoices(graph: ScenarioGraph | undefined): EndpointChoice[] {
  if (!graph) return [];
  const seen = new Set<string>();
  const out: EndpointChoice[] = [];

  for (const node of graph.nodes) {
    if (node.kind !== "http_request") continue;
    const name = `${node.method ?? "GET"} ${node.path ?? "/"}`;
    if (seen.has(name)) continue;
    seen.add(name);
    out.push({ name, usable: !/[{}\r\n]/.test(name) });
  }
  return out;
}
