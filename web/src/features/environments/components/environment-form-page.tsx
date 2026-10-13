import Link from "next/link";

import type { Project } from "@/features/projects/types";

import type { Environment } from "../types";
import { EnvironmentForm } from "./environment-form";

// EnvironmentFormPage frames the declaration form, or the edit form when
// given an environment.
export function EnvironmentFormPage({ project, environment }: { project: Project; environment?: Environment }) {
  return (
    <div className="flex flex-col gap-8">
      <header className="flex flex-col gap-1">
        <Link href={`/projects/${project.id}/environments`} className="text-xs text-muted-foreground hover:underline">
          {project.name} · Environments
        </Link>
        <h1 className="text-3xl font-normal tracking-tight">
          {environment ? `Edit ${environment.name}` : "New environment"}
        </h1>
        {!environment && (
          <p className="max-w-xl text-sm text-muted-foreground">
            It arrives not verified. You then serve a token on it to prove it is yours, and an admin of the project
            approves it.
          </p>
        )}
      </header>
      <EnvironmentForm projectId={project.id} environment={environment} />
    </div>
  );
}
