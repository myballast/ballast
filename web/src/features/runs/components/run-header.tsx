import { Hourglass } from "lucide-react";

import { formatClock, formatDuration } from "@/lib/format";
import { can } from "@/lib/permissions";

import type { RunContext } from "../detail";
import { stoppable, type Run } from "../types";
import { CancelRun } from "./cancel-run";
import { RerunRun } from "./rerun-run";
import { RunStatusBadge, RunStatusIcon } from "./run-status-badge";

// The facts of the run in the monospace face the design uses for them, in the
// order a person reading a failed run asks them: which run, who, when, how long.
function facts(run: Run, now: Date): string[] {
  const parts = [`run #${run.number}`];

  if (run.triggered_by) parts.push(`triggered by ${run.triggered_by}`);
  if (run.started_at) {
    parts.push(`started ${formatClock(run.started_at, now)}`);
  } else {
    parts.push(`queued ${formatClock(run.created_at, now)}`);
  }
  if (run.ended_at) {
    parts.push(`finished ${formatClock(run.ended_at, now)}`);
    parts.push(formatDuration(run.started_at, run.ended_at));
  }
  return parts;
}

// RunHeader says which run this is: the scenario it ran and how it ended, who
// started it and when, what it was run against, and what can be done with it —
// repeat it, or stop it while it is under way. Everything that describes the
// execution is read from the run or from the scenario version it names, both
// frozen.
export function RunHeader({ run, context, now }: { run: Run; context: RunContext; now: Date }) {
  const { project, scenario, environment, access } = context;
  const p = run.params;
  const stoppableNow = stoppable.includes(run.status);

  const pills = [
    { k: "application", v: project.name },
    { k: "tool", v: "k6" },
    { k: "profile", v: run.profile },
    { k: "target", v: environment?.name ?? "Deleted environment" },
    { k: "vus", v: String(p.target_vus) },
  ];

  return (
    <header className="flex flex-col gap-3">
      <div className="flex flex-wrap items-center gap-x-3 gap-y-3">
        <RunStatusIcon status={run.status} />
        <h1 className="text-3xl font-normal tracking-tight">{scenario?.name ?? "Deleted scenario"}</h1>
        <RunStatusBadge status={run.status} />

        {/* Hidden from whoever cannot use them; the API refuses them anyway. */}
        <div className="ml-auto flex flex-wrap items-start gap-2">
          {!stoppableNow && can(access, "run_test") && (
            <RerunRun
              runId={run.id}
              scenario={scenario?.name ?? "this scenario"}
              version={run.scenario_version}
              profile={run.profile}
              load={`${p.target_vus} VUs · ${p.ramp_up_s}s / ${p.plateau_s}s / ${p.ramp_down_s}s`}
            />
          )}
          {stoppableNow && !run.cancel_requested_at && can(access, "cancel_run") && (
            <CancelRun runId={run.id} running={run.status === "running"} />
          )}
        </div>
      </div>

      <p className="data text-[11.5px] leading-relaxed text-muted-foreground">{facts(run, now).join(" · ")}</p>

      <ul className="flex flex-wrap gap-1.5">
        {pills.map((pill) => (
          <li key={pill.k} className="data flex gap-1.5 rounded-md border px-2 py-0.5 text-[11px]">
            <span className="text-muted-foreground">{pill.k}:</span>
            <span>{pill.v}</span>
          </li>
        ))}
      </ul>

      {run.cancel_requested_at && stoppableNow && (
        <p className="flex items-center gap-2 rounded-lg border bg-card px-3 py-2 text-sm text-muted-foreground">
          <Hourglass aria-hidden className="size-4 shrink-0" />
          <span>
            Stop requested at <span className="num">{formatClock(run.cancel_requested_at, now)}</span>. The worker is
            stopping the load; the run ends as cancelled in a few seconds.
          </span>
        </p>
      )}

      {run.error_message && (
        <p className="data rounded-lg border bg-card px-3 py-2 text-xs text-muted-foreground">{run.error_message}</p>
      )}
    </header>
  );
}
