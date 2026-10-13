import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { formatMs } from "@/lib/format";

import { noResults, phases, trend, type Results } from "../metrics";
import type { Metric } from "../types";

// HttpPhases breaks a request down into the six phases k6 times on their own,
// to tell a slow server (waiting) from a slow network (connecting, tls) or a
// starved client (blocked). The bar is each phase's share of the average time
// spent in all six: a proportion, not a verdict, so it is neutral.
export function HttpPhases({ metrics, results }: { metrics: Metric[]; results: Results }) {
  const rows = phases.map((phase) => ({ ...phase, stats: trend(metrics, phase.metric) }));
  const totalAvg = rows.reduce((sum, row) => sum + (row.stats.avg ?? 0), 0);

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-[11px] font-normal tracking-wider text-muted-foreground uppercase">
          HTTP phases
        </CardTitle>
      </CardHeader>
      <CardContent>
        {rows.every((row) => row.stats.avg === null) ? (
          <p className="text-sm text-muted-foreground">
            {noResults(results, "Timings") ?? "No HTTP request was timed in this run."}
          </p>
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full min-w-[36rem] text-sm">
              <thead>
                <tr className="text-left text-[11px] tracking-wider text-muted-foreground uppercase">
                  <th className="py-2 pr-4 font-normal">Phase</th>
                  <th className="w-1/4 py-2 pr-4 font-normal">
                    <span className="sr-only">Share of the average</span>
                  </th>
                  {["avg", "med", "p95", "p99", "max"].map((stat) => (
                    <th key={stat} className="py-2 pl-3 text-right font-normal">
                      {stat}
                    </th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {rows.map((row) => {
                  const share = totalAvg > 0 && row.stats.avg !== null ? row.stats.avg / totalAvg : 0;
                  return (
                    <tr key={row.metric} className="border-t">
                      <td className="py-2 pr-4">
                        {row.label}
                        <span className="block text-xs text-muted-foreground">{row.hint}</span>
                      </td>
                      <td className="py-2 pr-4">
                        <div
                          className="h-1.5 w-full rounded-full bg-muted"
                          role="img"
                          aria-label={`${Math.round(share * 100)} % of the average`}
                        >
                          <div className="h-full rounded-full bg-foreground/60" style={{ width: `${share * 100}%` }} />
                        </div>
                      </td>
                      {(["avg", "med", "p95", "p99", "max"] as const).map((stat) => (
                        <td key={stat} className="num py-2 pl-3 text-right text-xs">
                          {formatMs(row.stats[stat])}
                        </td>
                      ))}
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        )}
      </CardContent>
    </Card>
  );
}
