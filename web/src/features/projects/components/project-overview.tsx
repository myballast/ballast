import { Lock, Play } from "lucide-react";
import Link from "next/link";

import { buttonVariants } from "@/components/ui/button";
import { EnvironmentList } from "@/features/environments/components/environment-list";
import type { Environment } from "@/features/environments/types";
import { RunHistory } from "@/features/runs/components/run-history";
import type { HistoryPage, Names } from "@/features/runs/history";
import { ScenarioList } from "@/features/scenarios/components/scenario-list";
import type { ScenarioSummary } from "@/features/scenarios/types";
import { can, type Access } from "@/lib/permissions";

import type { Project } from "../types";

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

export function ProjectOverview({
  project,
  access,
  scenarios,
  environments,
  history,
}: {
  project: Project;
  access: Access;
  scenarios: ScenarioSummary[];
  environments: Environment[];
  history: HistoryPage;
}) {
  const names: Names = {
    scenarios: Object.fromEntries(scenarios.map((s) => [s.id, s.name])),
    environments: Object.fromEntries(environments.map((e) => [e.id, e.name])),
  };

  return (
    <div className="flex flex-col gap-10">
      <ProjectHeader project={project} access={access} />

      <div className="grid gap-8 lg:grid-cols-2">
        <Section title="Scenarios">
          <ScenarioList scenarios={scenarios} />
        </Section>
        <Section
          title="Environments"
          link={
            <Link href={`/projects/${project.id}/environments`} className="text-xs text-muted-foreground hover:underline">
              Manage
            </Link>
          }
        >
          <EnvironmentList projectId={project.id} environments={environments} />
        </Section>
      </div>

      <Section title="Runs">
        <RunHistory projectId={project.id} names={names} first={history} />
      </Section>
    </div>
  );
}
