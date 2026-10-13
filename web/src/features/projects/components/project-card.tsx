import { CircleDashed, Globe, Lock, ShieldCheck } from "lucide-react";
import Link from "next/link";

import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import type { Environment } from "@/features/environments/types";
import { RunStatusBadge } from "@/features/runs/components/run-status-badge";
import type { RunSummary } from "@/features/runs/types";
import { formatRelative } from "@/lib/format";

import type { ProjectOverview } from "../types";

// ProjectCard is one project of the list: its name, its environments, and how
// its last run ended.
//
// An environment's verification is not the status of a run or of a
// threshold, so it is never green or amber: it is counted, in neutral text,
// with an icon and a word.
export function ProjectCard({ overview, now }: { overview: ProjectOverview; now: Date }) {
  const { project } = overview;

  return (
    <Card>
      <CardHeader>
        <CardTitle className="truncate">{project.name}</CardTitle>
        <p className="data truncate text-xs text-muted-foreground">{project.slug}</p>
      </CardHeader>

      <CardContent className="flex flex-col gap-3 text-sm">
        {overview.access === "restricted" ? (
          <p className="flex items-center gap-2 text-muted-foreground">
            <Lock aria-hidden className="size-4 shrink-0" />
            Your role in this project does not include its details.
          </p>
        ) : (
          <>
            <Environments environments={overview.environments} />
            <LastRun run={overview.lastRun} now={now} />
          </>
        )}
      </CardContent>
    </Card>
  );
}

function Environments({ environments }: { environments: Environment[] }) {
  if (environments.length === 0) {
    return (
      <p className="flex items-center gap-2 text-muted-foreground">
        <Globe aria-hidden className="size-4 shrink-0" />
        No environment yet
      </p>
    );
  }

  const verified = environments.filter((e) => e.verification_status === "verified").length;

  return (
    <div className="flex flex-col gap-1.5">
      <p className="flex flex-wrap items-center gap-x-2 gap-y-1">
        <Globe aria-hidden className="size-4 shrink-0 text-muted-foreground" />
        <span className="whitespace-nowrap">
          <span className="num">{environments.length}</span>{" "}
          {environments.length === 1 ? "environment" : "environments"}
        </span>
        <span className="text-muted-foreground">·</span>
        <span className="flex items-center gap-1 whitespace-nowrap text-muted-foreground">
          <ShieldCheck aria-hidden className="size-3.5" />
          <span className="num">{verified}</span> verified
        </span>
      </p>
      <ul className="flex flex-wrap gap-1.5">
        {environments.map((e) => (
          <li key={e.id} className="data rounded-md border px-2 py-0.5 text-[11px] text-muted-foreground">
            {e.name}
          </li>
        ))}
      </ul>
    </div>
  );
}

function LastRun({ run, now }: { run: RunSummary | null; now: Date }) {
  if (!run) {
    return (
      <p className="flex items-center gap-2 text-muted-foreground">
        <CircleDashed aria-hidden className="size-4 shrink-0" />
        Never run
      </p>
    );
  }

  return (
    <div className="flex flex-wrap items-center gap-2">
      <span className="text-muted-foreground">Last run</span>
      <RunStatusBadge status={run.status} />
      <span className="text-xs text-muted-foreground">{formatRelative(run.ended_at ?? run.created_at, now)}</span>
      <Link href={`/runs/${run.id}`} className="ml-auto text-xs text-brand hover:underline">
        View run
      </Link>
    </div>
  );
}
