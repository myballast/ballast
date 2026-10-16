import { Hourglass } from "lucide-react";
import Link from "next/link";

import { formatDateTime, formatDuration } from "@/lib/format";
import { can } from "@/lib/permissions";

import type { RunContext } from "../detail";
import { stoppable, type Run } from "../types";
import { CancelRun } from "./cancel-run";
import { RunStatusBadge } from "./run-status-badge";

function Fact({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="flex flex-col gap-1">
      <dt className="text-[11px] tracking-wider text-muted-foreground uppercase">{label}</dt>
      <dd className="text-sm">{children}</dd>
    </div>
  );
}

// RunHeader says what was run: the scenario version it executed, against
// which environment, with which load. Everything here that describes the
// execution is read from the run or from that version, both frozen.
export function RunHeader({ run, context }: { run: Run; context: RunContext }) {
  const { project, scenario, environment, access } = context;
  const p = run.params;
  const total = p.ramp_up_s + p.plateau_s + p.ramp_down_s;
  const newer = scenario && scenario.version !== run.scenario_version;

  return (
    <header className="flex flex-col gap-5">
      <div className="flex flex-wrap items-start justify-between gap-4">
        <div className="flex flex-col gap-2">
          <div className="flex flex-wrap items-center gap-3">
            <h1 className="text-3xl font-normal tracking-tight">Run <span className="num">#{run.number}</span></h1>
            <RunStatusBadge status={run.status} />
          </div>
          <span className="data text-xs break-all text-muted-foreground">{run.id}</span>
        </div>

        {/* Hidden from whoever cannot stop it; the API refuses them anyway. */}
        {stoppable.includes(run.status) && !run.cancel_requested_at && can(access, "cancel_run") && (
          <CancelRun runId={run.id} running={run.status === "running"} />
        )}
      </div>

      {run.cancel_requested_at && stoppable.includes(run.status) && (
        <p className="flex items-center gap-2 rounded-lg border bg-card px-3 py-2 text-sm text-muted-foreground">
          <Hourglass aria-hidden className="size-4 shrink-0" />
          <span>
            Stop requested at <span className="num">{formatDateTime(run.cancel_requested_at)}</span>. The worker
            is stopping the load; the run ends as cancelled in a few seconds.
          </span>
        </p>
      )}

      <dl className="grid grid-cols-2 gap-x-6 gap-y-4 md:grid-cols-4">
        <Fact label="Project">
          <Link href={`/projects/${project.id}`} className="hover:underline">
            {project.name}
          </Link>
        </Fact>
        <Fact label="Scenario">
          <span>{scenario?.name ?? <span className="data text-xs">{run.scenario_id.slice(0, 8)}</span>}</span>{" "}
          <span className="data text-xs text-muted-foreground">
            v{run.scenario_version} executed
            {newer && ` · now v${scenario.version}`}
            {!scenario && " · deleted since"}
          </span>
        </Fact>
        <Fact label="Environment">
          {environment?.name ?? <span className="data text-xs">{run.environment_id.slice(0, 8)}</span>}
        </Fact>
        <Fact label="Profile">{run.profile}</Fact>
        <Fact label="Load">
          <span className="num">
            {p.target_vus} VUs · {p.ramp_up_s}s / {p.plateau_s}s / {p.ramp_down_s}s
          </span>
          <span className="block text-xs text-muted-foreground">
            ramp-up / plateau / ramp-down, <span className="num">{total}</span> s in all
          </span>
        </Fact>
        <Fact label="Created">
          <span className="num">{formatDateTime(run.created_at)}</span>
        </Fact>
        <Fact label="Started">
          <span className="num">{formatDateTime(run.started_at)}</span>
        </Fact>
        <Fact label="Finished">
          <span className="num">{formatDateTime(run.ended_at)}</span>
          {run.started_at && run.ended_at && (
            <span className="num block text-xs text-muted-foreground">
              {formatDuration(run.started_at, run.ended_at)}
            </span>
          )}
        </Fact>
      </dl>

      {run.error_message && (
        <p className="data rounded-lg border bg-card px-3 py-2 text-xs text-muted-foreground">{run.error_message}</p>
      )}
    </header>
  );
}
