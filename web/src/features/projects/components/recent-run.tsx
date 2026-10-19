import Link from "next/link";

import { Time } from "@/components/time";
import { Card } from "@/components/ui/card";
import type { RunRow } from "@/features/runs/list";
import { RunStatusBadge } from "@/features/runs/components/run-status-badge";

// RecentRun is the project's latest run in a line: how it ended, which run it
// was, of what, when, and for how long. The steps of a run — setup, auth,
// browse — are not here: they need the run cut into steps, which is not
// something Ballast records yet.
export function RecentRun({ run, now }: { run: RunRow; now: Date }) {
  return (
    <Card className="gap-0 px-4 py-4">
      <div className="flex flex-wrap items-center gap-x-3 gap-y-2 text-sm">
        <span className="text-muted-foreground">Most recent run</span>
        <RunStatusBadge status={run.status} />
        <span className="num text-xs text-muted-foreground">
          #{run.number} · {run.scenario} · <Time iso={run.createdAt} mode="relative" now={now} /> · {run.duration}
        </span>
        <Link href={`/runs/${run.id}`} className="ml-auto text-xs text-brand hover:underline">
          Open run →
        </Link>
      </div>
    </Card>
  );
}
