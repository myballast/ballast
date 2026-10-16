import { FilePlus, Lock, Play } from "lucide-react";
import Link from "next/link";

import { EmptyState } from "@/components/states";
import { buttonVariants } from "@/components/ui/button";
import type { Environment } from "@/features/environments/types";
import { RunsTable } from "@/features/runs/components/runs-table";
import type { RunRow } from "@/features/runs/list";
import type { ScenarioListItem } from "@/features/scenarios/types";
import { can, type Access } from "@/lib/permissions";

import { tilesOf, type ProjectStats } from "../stats";
import type { Project } from "../types";
import { ProjectTabs } from "./project-tabs";
import { RecentRun } from "./recent-run";
import { StatTiles } from "./stat-tiles";

function Section({ title, link, children }: { title: string; link?: React.ReactNode; children: React.ReactNode }) {
  return (
    <section className="flex flex-col gap-3">
      <div className="flex items-baseline justify-between gap-3">
        <h2 className="text-[11px] tracking-wider text-muted-foreground uppercase">{title}</h2>
        {link}
      </div>
      {children}
    </section>
  );
}

export function ProjectHeader({ project, access }: { project: Project; access: Access }) {
  return (
    <header className="flex flex-wrap items-start justify-between gap-4">
      <div className="flex flex-col gap-1">
        <h1 className="text-3xl font-normal tracking-tight">{project.name}</h1>
        <p className="data text-xs text-muted-foreground">
          {project.slug} · your role: {access.role}
          {access.custom && " (custom permissions)"}
        </p>
      </div>
      {/* Hidden from whoever cannot launch; the API refuses them anyway. */}
      {can(access, "run_test") && (
        <Link href={`/projects/${project.id}/runs/new`} className={buttonVariants()}>
          <Play aria-hidden />
          Run a test
        </Link>
      )}
    </header>
  );
}

export function RestrictedProject() {
  return (
    <p className="flex items-center gap-2 text-sm text-muted-foreground">
      <Lock aria-hidden className="size-4" />
      Your role in this project does not include its details.
    </p>
  );
}

// What the project is made of, said in the shape of the design's tags. They are
// counted from what the project holds, not typed in: a tag that nobody can edit
// would say what somebody once guessed.
function Facts({ scenarios, environments }: { scenarios: ScenarioListItem[]; environments: Environment[] }) {
  const verified = environments.filter((e) => e.verification_status === "verified").length;
  const facts = [
    { k: "tool", v: "k6" },
    { k: "scenarios", v: String(scenarios.length) },
    { k: "environments", v: `${environments.length} (${verified} verified)` },
  ];

  return (
    <ul className="flex flex-wrap gap-1.5">
      {facts.map((fact) => (
        <li key={fact.k} className="data flex gap-1.5 rounded-md border px-2 py-0.5 text-[11px]">
          <span className="text-muted-foreground">{fact.k}:</span>
          <span>{fact.v}</span>
        </li>
      ))}
    </ul>
  );
}

export function ProjectOverview({
  project,
  access,
  scenarios,
  environments,
  stats,
  rows,
}: {
  project: Project;
  access: Access;
  scenarios: ScenarioListItem[];
  environments: Environment[];
  stats: ProjectStats;
  // The latest runs, newest first.
  rows: RunRow[];
}) {
  const launch = can(access, "run_test");
  const noRunInWindow = stats.total_runs.value === 0;

  return (
    <div className="flex flex-col gap-8">
      <header className="flex flex-col gap-4">
        <div className="flex flex-wrap items-start justify-between gap-4">
          <h1 className="text-3xl font-normal tracking-tight">{project.name}</h1>
          {/* Hidden from whoever cannot use them; the API refuses them anyway. */}
          <div className="flex flex-wrap gap-2">
            {can(access, "manage_scenarios") && (
              <Link href={`/projects/${project.id}/scenarios/new`} className={buttonVariants({ variant: "outline" })}>
                <FilePlus aria-hidden />
                New scenario
              </Link>
            )}
            {launch && (
              <Link href={`/projects/${project.id}/runs/new`} className={buttonVariants()}>
                <Play aria-hidden />
                Run a test
              </Link>
            )}
          </div>
        </div>
        <Facts scenarios={scenarios} environments={environments} />
        <ProjectTabs projectId={project.id} />
      </header>

      <Section title={`Last ${stats.days} days`}>
        <StatTiles tiles={tilesOf(stats)} days={stats.days} />
        {noRunInWindow && (
          <p className="text-sm text-muted-foreground">
            {rows.length === 0
              ? "No run yet. Launch a first test to see how this project is doing."
              : `No run in the last ${stats.days} days. Launch a test to see how this project is doing.`}
          </p>
        )}
      </Section>

      {rows.length === 0 ? (
        <EmptyState
          icon={Play}
          title="No run yet"
          description="Runs appear here once a scenario has been launched against a verified environment."
          action={
            launch ? (
              <Link href={`/projects/${project.id}/runs/new`} className={buttonVariants()}>
                <Play aria-hidden />
                Run a test
              </Link>
            ) : undefined
          }
        />
      ) : (
        <>
          <RecentRun run={rows[0]} />
          <Section
            title="Latest runs"
            link={
              <Link href={`/projects/${project.id}/runs`} className="text-xs text-muted-foreground hover:underline">
                View all
              </Link>
            }
          >
            <RunsTable rows={rows} />
          </Section>
        </>
      )}
    </div>
  );
}
