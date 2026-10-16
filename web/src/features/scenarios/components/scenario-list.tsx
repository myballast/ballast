import { ChevronRight, FileCode } from "lucide-react";
import Link from "next/link";

import { EmptyState } from "@/components/states";
import { formatDateTime } from "@/lib/format";

import type { ScenarioListItem } from "../types";

export function ScenarioList({
  projectId,
  scenarios,
  action,
}: {
  projectId: string;
  scenarios: ScenarioListItem[];
  // What the empty state offers, if the caller may write one.
  action?: React.ReactNode;
}) {
  if (scenarios.length === 0) {
    return (
      <EmptyState
        icon={FileCode}
        title="No scenario yet"
        description="A scenario is a k6 script, with the load it starts from and the thresholds a run must meet."
        action={action}
      />
    );
  }

  return (
    <ul className="flex flex-col divide-y rounded-xl border">
      {scenarios.map((s) => (
        <li key={s.id}>
          <Link
            href={`/projects/${projectId}/scenarios/${s.id}`}
            className="flex items-center gap-3 px-4 py-3 text-sm hover:bg-muted/40"
          >
            <span className="flex min-w-0 flex-1 flex-col gap-1.5">
              <span className="flex flex-wrap items-baseline gap-x-3 gap-y-1">
                <span className="font-medium">{s.name}</span>
                <span className="data text-xs text-muted-foreground">v{s.version}</span>
              </span>
              <span className="flex flex-wrap gap-x-3 gap-y-1 text-xs text-muted-foreground">
                <span>default profile {s.default_profile}</span>
                <span>
                  <span className="num">{s.threshold_count}</span>{" "}
                  {s.threshold_count === 1 ? "threshold" : "thresholds"}
                </span>
                <span>
                  updated <span className="num">{formatDateTime(s.updated_at)}</span>
                </span>
              </span>
            </span>
            <ChevronRight aria-hidden className="size-4 shrink-0 text-muted-foreground" />
          </Link>
        </li>
      ))}
    </ul>
  );
}
