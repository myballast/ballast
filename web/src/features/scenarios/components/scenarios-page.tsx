import { FileUp, ListTree, Plus } from "lucide-react";
import Link from "next/link";

import { buttonVariants } from "@/components/ui/button";
import type { Project } from "@/features/projects/types";
import { can, type Access } from "@/lib/permissions";

import type { ScenarioListItem } from "../types";
import { ScenarioList } from "./scenario-list";

// ScenariosPage lists the project's scenarios: their current version, how many
// thresholds they impose, and when they last changed.
export function ScenariosPage({
  project,
  access,
  scenarios,
}: {
  project: Project;
  access: Access;
  scenarios: ScenarioListItem[];
}) {
  // Hidden from whoever cannot write one; the API refuses them anyway.
  const create = can(access, "manage_scenarios") ? (
    <div className="flex flex-wrap gap-2">
      <Link href={`/projects/${project.id}/scenarios/import`} className={buttonVariants({ variant: "outline" })}>
        <FileUp aria-hidden />
        Import OpenAPI
      </Link>
      <Link href={`/projects/${project.id}/scenarios/new`} className={buttonVariants()}>
        <Plus aria-hidden />
        New scenario
      </Link>
    </div>
  ) : undefined;

  return (
    <div className="flex flex-col gap-8">
      <header className="flex flex-wrap items-start justify-between gap-4">
        <div className="flex flex-col gap-1">
          <Link href={`/projects/${project.id}`} className="text-xs text-muted-foreground hover:underline">
            {project.name}
          </Link>
          <h1 className="text-3xl font-normal tracking-tight">Scenarios</h1>
          <p className="max-w-xl text-sm text-muted-foreground">
            A scenario is the k6 script a run executes. Each change to its script or its defaults is a new version, and
            a run always keeps the version it executed.
          </p>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <Link href={`/projects/${project.id}/endpoints`} className={buttonVariants({ variant: "ghost" })}>
            <ListTree aria-hidden />
            Endpoints
          </Link>
          {scenarios.length > 0 && create}
        </div>
      </header>
      <ScenarioList projectId={project.id} scenarios={scenarios} action={create} />
    </div>
  );
}
