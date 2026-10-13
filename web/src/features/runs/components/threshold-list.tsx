import { CircleCheck, CircleX } from "lucide-react";

import { StatusBadge } from "@/components/status-badge";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";

import { noResults, type Results } from "../metrics";
import type { Threshold } from "../types";

// ThresholdList shows k6's own verdicts. Ballast never recomputes them.
export function ThresholdList({ thresholds, results }: { thresholds: Threshold[]; results: Results }) {
  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-[11px] font-normal tracking-wider text-muted-foreground uppercase">
          Thresholds
        </CardTitle>
      </CardHeader>
      <CardContent>
        {thresholds.length === 0 ? (
          <p className="text-sm text-muted-foreground">
            {/* What the run was held to is frozen on the run: "this run", not
                "this scenario", which may have changed since. */}
            {noResults(results, "Verdicts") ?? "This run had no thresholds."}
          </p>
        ) : (
          <ul className="divide-y">
            {thresholds.map((threshold) => (
              <li
                key={`${threshold.metric} ${threshold.expression}`}
                className="flex items-center gap-3 py-2"
              >
                {threshold.passed ? (
                  <StatusBadge
                    variant="inline"
                    tone="passed"
                    icon={CircleCheck}
                    label="Passed"
                    className="w-20"
                  />
                ) : (
                  <StatusBadge
                    variant="inline"
                    tone="failed"
                    icon={CircleX}
                    label="Failed"
                    className="w-20"
                  />
                )}
                <span className="data text-xs">{threshold.metric}</span>
                <span className="data ml-auto text-xs text-muted-foreground">
                  {threshold.expression}
                </span>
              </li>
            ))}
          </ul>
        )}
      </CardContent>
    </Card>
  );
}
