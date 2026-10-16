import Link from "next/link";

import type { Project } from "@/features/projects/types";

import { ScenarioForm } from "./scenario-form";

// NewScenarioPage frames the creation form. The thresholds come after: they
// belong to a scenario, which exists once this is saved.
export function NewScenarioPage({ project }: { project: Project }) {
  return (
    <div className="flex flex-col gap-8">
      <header className="flex flex-col gap-1">
        <Link href={`/projects/${project.id}/scenarios`} className="text-xs text-muted-foreground hover:underline">
          {project.name} · Scenarios
        </Link>
        <h1 className="text-3xl font-normal tracking-tight">New scenario</h1>
        <p className="max-w-xl text-sm text-muted-foreground">
          It starts from a script that already passes Ballast&apos;s checks. Once it is saved, you can add its
          thresholds.
        </p>
      </header>
      <ScenarioForm projectId={project.id} />
    </div>
  );
}
