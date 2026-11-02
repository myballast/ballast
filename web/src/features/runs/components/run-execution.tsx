import Link from "next/link";

import { InfoTip } from "@/components/info-tip";
import { Time } from "@/components/time";
import { formatDuration } from "@/lib/format";

import type { RunContext } from "../detail";
import { loadHelp } from "../metric-help";
import { profileHelp } from "../profile-help";
import type { Run } from "../types";

function Fact({
  label,
  help,
  children,
}: {
  label: string;
  help?: string;
  children: React.ReactNode;
}) {
  return (
    <div className="flex flex-col gap-1">
      <dt className="flex items-center gap-1.5 text-[11px] tracking-wider text-muted-foreground uppercase">
        {label}
        {help && <InfoTip label={label}>{help}</InfoTip>}
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
        {/* The whole line leads to the scenario, while it still exists. */}
        {scenario ? (
          <Link
            href={`/projects/${project.id}/scenarios/${scenario.id}`}
            title="Open the scenario"
            className="-mx-1 block rounded-md px-1 hover:bg-muted focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none"
          >
            <span className="underline-offset-4 hover:underline">{scenario.name}</span>{" "}
            <span className="data text-xs text-muted-foreground">
              v{run.scenario_version} executed
              {newer && ` · now v${scenario.version}`}
            </span>
          </Link>
        ) : (
          <>
            <span>Deleted scenario</span>{" "}
            <span className="data text-xs text-muted-foreground">v{run.scenario_version} executed · deleted since</span>
          </>
        )}
      </Fact>
      <Fact label="Environment">
        {environment?.name ?? "Deleted environment"}
      </Fact>
      <Fact label="Profile" help={profileHelp[run.profile]?.summary}>
        {run.profile}
      </Fact>
      <Fact label="Load" help={loadHelp}>
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
        <span className="num"><Time iso={run.created_at} /></span>
      </Fact>
      <Fact label="Started">
        <span className="num"><Time iso={run.started_at} /></span>
      </Fact>
      <Fact label="Finished">
        <span className="num"><Time iso={run.ended_at} /></span>
        {run.started_at && run.ended_at && (
          <span className="num block text-xs text-muted-foreground">
            {formatDuration(run.started_at, run.ended_at)}
          </span>
        )}
      </Fact>
    </dl>
  );
}
