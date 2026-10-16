import Link from "next/link";

import { formatDateTime, formatDuration } from "@/lib/format";

import type { RunContext } from "../detail";
import type { Run } from "../types";

function Fact({
  label,
  children,
}: {
  label: string;
  children: React.ReactNode;
}) {
  return (
    <div className="flex flex-col gap-1">
      <dt className="text-[11px] tracking-wider text-muted-foreground uppercase">
        {label}
      </dt>
      <dd className="text-sm">{children}</dd>
    </div>
  );
}

// RunExecution is what this run executed, said in full: the scenario version it
// names against the one the scenario is at now, the profile and the load frozen
// on the run, and its dates. The header says it in a line; this is the account
// to read when the line is not enough, and it is read from the run and from the
// version it names, both frozen.
export function RunExecution({
  run,
  context,
}: {
  run: Run;
  context: RunContext;
}) {
  const { project, scenario, environment } = context;
  const p = run.params;
  const total = p.ramp_up_s + p.plateau_s + p.ramp_down_s;
  const newer = scenario && scenario.version !== run.scenario_version;

  return (
    <dl className="grid grid-cols-2 gap-x-6 gap-y-4 md:grid-cols-4">
      <Fact label="Project">
        <Link href={`/projects/${project.id}`} className="hover:underline">
          {project.name}
        </Link>
      </Fact>
      <Fact label="Scenario">
        <span>{scenario?.name ?? "Deleted scenario"}</span>{" "}
        <span className="data text-xs text-muted-foreground">
          v{run.scenario_version} executed
          {newer && ` · now v${scenario.version}`}
          {!scenario && " · deleted since"}
        </span>
      </Fact>
      <Fact label="Environment">
        {environment?.name ?? "Deleted environment"}
      </Fact>
      <Fact label="Profile">{run.profile}</Fact>
      <Fact label="Load">
        <span className="num">
          {p.target_vus} VUs · {p.ramp_up_s}s / {p.plateau_s}s / {p.ramp_down_s}
          s
        </span>
        <span className="block text-xs text-muted-foreground">
          ramp-up / plateau / ramp-down, <span className="num">{total}</span> s
          in all
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
  );
}
