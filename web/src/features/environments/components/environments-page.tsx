import { Plus } from "lucide-react";
import Link from "next/link";

import { buttonVariants } from "@/components/ui/button";
import type { Project } from "@/features/projects/types";
import { can, type Access } from "@/lib/permissions";

import type { Environment } from "../types";
import { EnvironmentList } from "./environment-list";

function NewEnvironment({ projectId }: { projectId: string }) {
  return (
    <Link href={`/projects/${projectId}/environments/new`} className={buttonVariants()}>
      <Plus aria-hidden />
      New environment
    </Link>
  );
}

// EnvironmentsPage lists the project's targets, with how far each one's
// verification has gone and the ceilings runs against it are clamped to.
export function EnvironmentsPage({
  project,
  access,
  environments,
}: {
  project: Project;
  access: Access;
  environments: Environment[];
}) {
  // Hidden from whoever cannot declare one; the API refuses them anyway.
  const declare = can(access, "manage_environments") ? <NewEnvironment projectId={project.id} /> : undefined;

  return (
    <div className="flex flex-col gap-8">
      <header className="flex flex-wrap items-start justify-between gap-4">
        <div className="flex flex-col gap-1">
          <Link href={`/projects/${project.id}`} className="text-xs text-muted-foreground hover:underline">
            {project.name}
          </Link>
          <h1 className="text-3xl font-normal tracking-tight">Environments</h1>
          <p className="max-w-xl text-sm text-muted-foreground">
            A run only sends load to an environment once somebody has proved they own it and an admin of the project
            has approved it.
          </p>
        </div>
        {environments.length > 0 && declare}
      </header>
      <EnvironmentList
        projectId={project.id}
        environments={environments}
        canManage={can(access, "manage_environments")}
        now={new Date()}
        action={declare}
      />
    </div>
  );
}
