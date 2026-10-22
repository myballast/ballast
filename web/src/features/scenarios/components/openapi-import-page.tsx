import Link from "next/link";

import type { Project } from "@/features/projects/types";

import { OpenAPIImport } from "./openapi-import";

// OpenAPIImportPage frames the import of a specification. The scenario it makes
// is written as a graph, so it is read only here: the requests are kept as they
// come and ordered later.
export function OpenAPIImportPage({ project }: { project: Project }) {
  return (
    <div className="flex flex-col gap-8">
      <header className="flex flex-col gap-1">
        <Link href={`/projects/${project.id}/scenarios`} className="text-xs text-muted-foreground hover:underline">
          {project.name} · Scenarios
        </Link>
        <h1 className="text-3xl font-normal tracking-tight">Import from OpenAPI</h1>
        <p className="max-w-xl text-sm text-muted-foreground">
          Upload an OpenAPI 3.x or Swagger 2.0 file, in JSON or YAML, and tick the endpoints to test. Each one becomes
          a request in a new scenario. The requests are independent: a specification lists what exists, not the order
          a user goes through it.
        </p>
      </header>
      <OpenAPIImport projectId={project.id} />
    </div>
  );
}
