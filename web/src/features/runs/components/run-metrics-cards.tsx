import { CircleCheck, CircleX } from "lucide-react";

import { MetricCard } from "@/components/metric-card";
import { formatBytes, formatCount, formatMs, formatPercent, formatRate } from "@/lib/format";

import { closeness, verdictsByCard, type CardId, type CardRule } from "../card-verdicts";
import { errorRateNotice, metricHelp } from "../metric-help";
import { headlineMetrics } from "../metrics";
import type { Metric, Threshold } from "../types";

const symbols: Record<string, string> = { "<=": "≤", ">=": "≥", "==": "=", "!=": "≠" };

// RunMetricsCards is the headline numbers of a run. A number a threshold judges
// carries that threshold's verdict, with the rule it was held to: k6's own, read
// from the run, never recomputed here.
export function RunMetricsCards({ metrics, thresholds }: { metrics: Metric[]; thresholds: Threshold[] }) {
  const h = headlineMetrics(metrics);
  const verdicts = verdictsByCard(thresholds);

  // Each card says its limit the way it says its number.
  const cards: {
    id: CardId;
    label: string;
    value: string;
    raw: number | null;
    limit: (n: number) => string;
    hint?: string;
    notice?: string | null;
  }[] = [
    { id: "p95", label: "p95 latency", value: formatMs(h.p95Ms), raw: h.p95Ms, limit: formatMs },
    { id: "p99", label: "p99 latency", value: formatMs(h.p99Ms), raw: h.p99Ms, limit: formatMs },
    { id: "rps", label: "Requests / sec", value: formatRate(h.requestsPerSecond), raw: h.requestsPerSecond, limit: formatRate },
    {
      id: "errors",
      label: "Error rate",
      value: formatPercent(h.errorRate),
      raw: h.errorRate,
      limit: formatPercent,
      notice: errorRateNotice(h.errorRate, thresholds),
    },
    { id: "median", label: "Median latency", value: formatMs(h.medianMs), raw: h.medianMs, limit: formatMs },
    { id: "iterations", label: "Iterations", value: formatCount(h.iterations), raw: h.iterations, limit: formatCount },
    {
      id: "checks",
      label: "Checks passed",
      value: formatPercent(h.checksRate),
      raw: h.checksRate,
      limit: formatPercent,
      hint: h.checksRate === null ? "This scenario defines no checks" : undefined,
    },
    { id: "bytes", label: "Data received", value: formatBytes(h.bytesReceived), raw: h.bytesReceived, limit: formatBytes },
  ];

  return (
    <section className="grid grid-cols-2 gap-3.5 lg:grid-cols-4">
      {cards.map((card) => {
        const verdict = verdicts[card.id];
        // The bar follows the rule that failed, or else the first one.
        const shown: CardRule | undefined = verdict?.rules.find((r) => !r.passed) ?? verdict?.rules[0];

        return (
          <MetricCard
            key={card.label}
            label={card.label}
            value={card.value}
            hint={
              verdict
                ? `threshold ${verdict.rules.map((r) => `${symbols[r.operator] ?? r.operator} ${card.limit(r.limit)}`).join(" · ")}`
                : card.hint
            }
            notice={card.notice}
            help={metricHelp[card.label]}
            status={
              verdict
                ? verdict.passed
                  ? { tone: "passed", icon: CircleCheck, label: "Met" }
                  : { tone: "failed", icon: CircleX, label: "Breached" }
                : undefined
            }
            closeness={shown ? closeness(card.raw, shown) : null}
          />
        );
      })}
    </section>
  );
}
