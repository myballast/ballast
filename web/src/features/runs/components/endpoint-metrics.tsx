import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { formatCount, formatMs, formatPercent } from "@/lib/format";

import { endpointMetrics, noResults, type Results } from "../metrics";
import type { Metric } from "../types";

// EndpointMetrics shows the sub-metrics k6 reported, one row per tag selector.
//
// k6 computes a sub-metric only for a selector that a threshold names, so this
// lists the endpoints the scenario declared thresholds on, and says so when
// there are none rather than looking empty for no reason.
export function EndpointMetrics({ metrics, results }: { metrics: Metric[]; results: Results }) {
  const endpoints = endpointMetrics(metrics);

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-[11px] font-normal tracking-wider text-muted-foreground uppercase">
          By endpoint
        </CardTitle>
      </CardHeader>
      <CardContent>
        {results !== "reported" ? (
          <p className="text-sm text-muted-foreground">{noResults(results, "Endpoint measurements")}</p>
        ) : endpoints.length === 0 ? (
          <p className="text-sm text-muted-foreground">
            k6 measures an endpoint on its own only when a threshold names it. Add one to the scenario, such as{" "}
            <code className="data text-xs text-foreground">http_req_duration{"{name:POST /login}"}</code>, and its
            numbers appear here on the next run.
          </p>
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full min-w-[36rem] text-sm">
              <thead>
                <tr className="text-left text-[11px] tracking-wider text-muted-foreground uppercase">
                  <th className="py-2 pr-4 font-normal">Selector</th>
                  {["requests", "p95", "p99", "avg", "errors"].map((h) => (
                    <th key={h} className="py-2 pl-3 text-right font-normal">
                      {h}
                    </th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {endpoints.map((e) => (
                  <tr key={e.tag} className="border-t">
                    <td className="data py-2 pr-4 text-xs break-all">{e.tag}</td>
                    <td className="num py-2 pl-3 text-right text-xs">{formatCount(e.requests)}</td>
                    <td className="num py-2 pl-3 text-right text-xs">{formatMs(e.duration?.p95 ?? null)}</td>
                    <td className="num py-2 pl-3 text-right text-xs">{formatMs(e.duration?.p99 ?? null)}</td>
                    <td className="num py-2 pl-3 text-right text-xs">{formatMs(e.duration?.avg ?? null)}</td>
                    <td className="num py-2 pl-3 text-right text-xs">{formatPercent(e.failedRate)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </CardContent>
    </Card>
  );
}
