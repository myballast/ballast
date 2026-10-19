import Link from "next/link";

import { RunStatusBadge } from "@/features/runs/components/run-status-badge";
import type { RunSummary } from "@/features/runs/types";
import { formatRelative } from "@/lib/format";

// RunLine is the foot of a card and the last column of a row: how many
// environments the project has and how its last run ended, with the link to that
// run. The status goes through the badge — icon, word and colour together — and
// not through a coloured dot, which would say it with the colour alone.
export function RunLine({
  environments,
  run,
  now,
}: {
  environments: number;
  run: RunSummary | null;
  now: Date;
}) {
  const count = (
    <span className="whitespace-nowrap">
      <span className="num">{environments}</span> {environments === 1 ? "environment" : "environments"}
    </span>
  );

  if (!run) {
    return (
      <p className="flex flex-wrap items-center gap-x-2 gap-y-1 text-xs text-muted-foreground">
        {count}
        <span aria-hidden>·</span>
        <span>never run</span>
      </p>
    );
  }

  return (
    <p className="flex flex-wrap items-center gap-x-2 gap-y-1 text-xs text-muted-foreground">
      {count}
      <span aria-hidden>·</span>
      <span>last run</span>
      <RunStatusBadge status={run.status} />
      <span>{formatRelative(run.ended_at ?? run.created_at, now)}</span>
      <Link href={`/runs/${run.id}`} className="ml-auto text-brand hover:underline">
        View run
      </Link>
    </p>
  );
}
