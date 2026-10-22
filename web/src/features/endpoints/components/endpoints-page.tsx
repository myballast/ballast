import { FileUp, ListTree } from "lucide-react";
import Link from "next/link";

import { EmptyState } from "@/components/states";
import { buttonVariants } from "@/components/ui/button";
import type { Project } from "@/features/projects/types";
import { can, type Access } from "@/lib/permissions";

import type { CatalogEndpoint } from "../types";
import { EndpointCatalog } from "./endpoint-catalog";

// EndpointsPage shows what a project's catalog holds: the endpoints read from the
// specifications somebody imported, which a scenario can pick from.
export function EndpointsPage({
  project,
  access,
  endpoints,
}: {
  project: Project;
  access: Access;
  endpoints: CatalogEndpoint[];
}) {
  const canEdit = can(access, "manage_scenarios");

  // Hidden from whoever cannot write scenarios; the API refuses them anyway.
  const importLink = canEdit ? (
    <Link href={`/projects/${project.id}/scenarios/import`} className={buttonVariants({ variant: "outline" })}>
      <FileUp aria-hidden />
      Import requests
    </Link>
  ) : undefined;

  return (
    <div className="flex flex-col gap-8">
      <header className="flex flex-wrap items-start justify-between gap-4">
        <div className="flex flex-col gap-1">
          <Link href={`/projects/${project.id}/scenarios`} className="text-xs text-muted-foreground hover:underline">
            {project.name} · Scenarios
          </Link>
          <h1 className="text-3xl font-normal tracking-tight">Endpoints</h1>
          <p className="max-w-xl text-sm text-muted-foreground">
            What this project&apos;s specifications describe. When you write a scenario, you pick an endpoint from here
            instead of typing it. No credential is ever kept.
          </p>
        </div>
        {endpoints.length > 0 && importLink}
      </header>

      {endpoints.length === 0 ? (
        <EmptyState
          icon={ListTree}
          title="No endpoint yet"
          description="Import an OpenAPI specification or a Postman collection and add its endpoints to the project."
          action={importLink}
        />
      ) : (
        <EndpointCatalog projectId={project.id} endpoints={endpoints} canEdit={canEdit} />
      )}
    </div>
  );
}
