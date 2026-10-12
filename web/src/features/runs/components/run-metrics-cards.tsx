import { MetricCard } from "@/components/metric-card";
import { formatMs, formatPercent, formatRate } from "@/lib/format";

import { headlineMetrics } from "../metrics";
import type { Metric } from "../types";

export function RunMetricsCards({ metrics }: { metrics: Metric[] }) {
  const headline = headlineMetrics(metrics);

  const cards = [
    { label: "p95 latency", value: formatMs(headline.p95Ms) },
    { label: "p99 latency", value: formatMs(headline.p99Ms) },
    { label: "Requests / sec", value: formatRate(headline.requestsPerSecond) },
    { label: "Error rate", value: formatPercent(headline.errorRate) },
  ];

  return (
    <section className="grid grid-cols-2 gap-3.5 lg:grid-cols-4">
      {cards.map((card) => (
        <MetricCard key={card.label} label={card.label} value={card.value} />
      ))}
    </section>
  );
}
