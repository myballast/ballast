import { MetricCard } from "@/components/metric-card";
import { formatBytes, formatCount, formatMs, formatPercent, formatRate } from "@/lib/format";

import { headlineMetrics } from "../metrics";
import type { Metric } from "../types";

export function RunMetricsCards({ metrics }: { metrics: Metric[] }) {
  const h = headlineMetrics(metrics);

  const cards = [
    { label: "p95 latency", value: formatMs(h.p95Ms) },
    { label: "p99 latency", value: formatMs(h.p99Ms) },
    { label: "Requests / sec", value: formatRate(h.requestsPerSecond) },
    { label: "Error rate", value: formatPercent(h.errorRate) },
    { label: "Median latency", value: formatMs(h.medianMs) },
    { label: "Iterations", value: formatCount(h.iterations) },
    { label: "Checks passed", value: formatPercent(h.checksRate) },
    { label: "Data received", value: formatBytes(h.bytesReceived) },
  ];

  return (
    <section className="grid grid-cols-2 gap-3.5 lg:grid-cols-4">
      {cards.map((card) => (
        <MetricCard key={card.label} label={card.label} value={card.value} />
      ))}
    </section>
  );
}
