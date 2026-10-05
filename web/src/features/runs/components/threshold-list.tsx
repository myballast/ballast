import { CircleCheck, CircleX } from "lucide-react";

import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";

import type { Threshold } from "../types";

// ThresholdList shows k6's own verdicts. Ballast never recomputes them.
export function ThresholdList({ thresholds }: { thresholds: Threshold[] }) {
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
            This scenario declares no thresholds.
          </p>
        ) : (
          <ul className="divide-y">
            {thresholds.map((threshold) => (
              <li
                key={`${threshold.metric} ${threshold.expression}`}
                className="flex items-center gap-3 py-2"
              >
                {threshold.passed ? (
                  <span className="flex w-20 items-center gap-1.5 text-xs text-status-passed">
                    <CircleCheck aria-hidden className="size-3.5" />
                    Passed
                  </span>
                ) : (
                  <span className="flex w-20 items-center gap-1.5 text-xs text-status-failed">
                    <CircleX aria-hidden className="size-3.5" />
                    Failed
                  </span>
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
