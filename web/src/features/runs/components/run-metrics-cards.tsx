import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
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
        <Card key={card.label}>
          <CardHeader>
            <CardTitle className="text-[11px] font-normal tracking-wider text-muted-foreground uppercase">
              {card.label}
            </CardTitle>
          </CardHeader>
          <CardContent>
            <p className="data text-2xl">{card.value}</p>
          </CardContent>
        </Card>
      ))}
    </section>
  );
}
