import type { Threshold } from "./types.ts";

// Which card of a run a threshold speaks about, so that its verdict can be put on the
// number it judges. The verdict itself is k6's, read from the run: nothing here
// decides whether a rule held.

export type CardId = "p95" | "p99" | "median" | "rps" | "errors" | "iterations" | "checks" | "bytes";

// The card of a metric and a statistic. A rule on a tagged metric — one endpoint —
// is not about the run's whole number and has no card.
const cards: { metric: string; stat: string; card: CardId }[] = [
  { metric: "http_req_duration", stat: "p(95)", card: "p95" },
  { metric: "http_req_duration", stat: "p(99)", card: "p99" },
  { metric: "http_req_duration", stat: "med", card: "median" },
  { metric: "http_reqs", stat: "rate", card: "rps" },
  { metric: "http_req_failed", stat: "rate", card: "errors" },
  { metric: "iterations", stat: "count", card: "iterations" },
  { metric: "checks", stat: "rate", card: "checks" },
  { metric: "data_received", stat: "count", card: "bytes" },
];

export type Operator = "<" | "<=" | ">" | ">=" | "==" | "!=";

export type ParsedExpression = { stat: string; operator: Operator; limit: number };

// parseExpression reads k6's threshold syntax for the one form a card can show: a
// statistic, a comparison and a number, such as p(95)<800 or rate<0.01.
export function parseExpression(expression: string): ParsedExpression | null {
  const match = /^\s*(p\(\d+(?:\.\d+)?\)|avg|min|med|max|rate|count|value)\s*(<=|>=|===|==|!=|<|>)\s*(\d+(?:\.\d+)?(?:e[-+]?\d+)?)\s*$/i.exec(
    expression,
  );
  if (!match) return null;

  const limit = Number(match[3]);
  if (!Number.isFinite(limit)) return null;
  const operator = (match[2] === "===" ? "==" : match[2]) as Operator;
  return { stat: match[1].toLowerCase(), operator, limit };
}

export type CardRule = { expression: string; operator: Operator; limit: number; passed: boolean };

export type CardVerdict = {
  // Breached when any rule on the card did not hold.
  passed: boolean;
  rules: CardRule[];
};

export function verdictsByCard(thresholds: Threshold[]): Partial<Record<CardId, CardVerdict>> {
  const found: Partial<Record<CardId, CardVerdict>> = {};

  for (const threshold of thresholds) {
    const parsed = parseExpression(threshold.expression);
    if (!parsed) continue;
    const card = cards.find((c) => c.metric === threshold.metric && c.stat === parsed.stat)?.card;
    if (!card) continue;

    const verdict = found[card] ?? { passed: true, rules: [] };
    verdict.rules.push({ expression: threshold.expression, operator: parsed.operator, limit: parsed.limit, passed: threshold.passed });
    verdict.passed = verdict.passed && threshold.passed;
    found[card] = verdict;
  }
  return found;
}

// closeness is how far a number is toward the limit of a rule, from 0 to 1 and no
// further: a bar drawn from it is full once the limit is reached, whichever side it
// was meant to stay on. Null where a bar would say nothing.
export function closeness(value: number | null, rule: { operator: Operator; limit: number }): number | null {
  if (value === null || !(rule.limit > 0)) return null;
  if (rule.operator === "==" || rule.operator === "!=") return null;
  return Math.max(0, Math.min(1, value / rule.limit));
}
