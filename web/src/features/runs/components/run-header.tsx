import { formatDateTime, formatDuration } from "@/lib/format";

import type { Run } from "../types";
import { RunStatusBadge } from "./run-status-badge";

export function RunHeader({ run }: { run: Run }) {
  const pills: [string, string][] = [
    ["profile", run.profile],
    ["vus", String(run.params.target_vus)],
    [
      "stages",
      `${run.params.ramp_up_s}s / ${run.params.plateau_s}s / ${run.params.ramp_down_s}s`,
    ],
    ["scenario", `${run.scenario_id.slice(0, 8)} v${run.scenario_version}`],
    ["environment", run.environment_id.slice(0, 8)],
  ];

  return (
    <header className="flex flex-col gap-3">
      <div className="flex flex-wrap items-center gap-3">
        <h1 className="text-3xl font-normal tracking-tight">Run</h1>
        <span className="data text-sm text-muted-foreground">{run.id}</span>
        <RunStatusBadge status={run.status} />
      </div>

      <p className="data text-xs text-muted-foreground">
        created {formatDateTime(run.created_at)} · started{" "}
        {formatDateTime(run.started_at)} · finished{" "}
        {formatDateTime(run.ended_at)} ·{" "}
        {formatDuration(run.started_at, run.ended_at)}
      </p>

      <ul className="flex flex-wrap gap-1.5">
        {pills.map(([key, value]) => (
          <li
            key={key}
            className="data rounded-md border px-2 py-0.5 text-[11px]"
          >
            <span className="text-muted-foreground">{key}:</span> {value}
          </li>
        ))}
      </ul>

      {run.error_message && (
        <p className="data rounded-lg border bg-card px-3 py-2 text-xs text-muted-foreground">
          {run.error_message}
        </p>
      )}
    </header>
  );
}
