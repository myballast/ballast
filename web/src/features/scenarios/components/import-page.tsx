import Link from "next/link";

import type { Project } from "@/features/projects/types";
import { cn } from "@/lib/utils";

import { OpenAPIImport } from "./openapi-import";
import { PostmanImport } from "./postman-import";

export type ImportSource = "openapi" | "postman";

type Target = { id: string; name: string };

const sources: { id: ImportSource; label: string }[] = [
  { id: "openapi", label: "OpenAPI" },
  { id: "postman", label: "Postman" },
];

// ImportPage frames the imports of a scenario's requests: from an OpenAPI
// specification, which lists what exists, and from a Postman collection, which
// is a journey. The scenario either makes is written as a graph, so it is read
// only here and the requests are ordered later.
export function ImportPage({
  project,
  from,
  environments,
  canManageEnvironments,
}: {
  project: Project;
  from: ImportSource;
  // The environments somebody may create secrets in. Empty for whoever may not.
  environments: Target[];
  canManageEnvironments: boolean;
}) {
  return (
    <div className="flex flex-col gap-8">
      <header className="flex flex-col gap-1">
        <Link href={`/projects/${project.id}/scenarios`} className="text-xs text-muted-foreground hover:underline">
          {project.name} · Scenarios
        </Link>
        <h1 className="text-3xl font-normal tracking-tight">Import requests</h1>
        <p className="max-w-xl text-sm text-muted-foreground">
          {from === "openapi"
            ? "Upload an OpenAPI 3.x or Swagger 2.0 file, in JSON or YAML, and tick the endpoints to test. The requests are independent: a specification lists what exists, not the order a user goes through it."
            : "Upload a Postman collection, and its environment if it has one. The requests keep the order of the collection. No token or password of the files is copied: each becomes a secret you create on an environment."}
        </p>
      </header>

      <nav aria-label="Where the requests come from" className="flex gap-1 border-b">
        {sources.map((source) => (
          <Link
            key={source.id}
            href={`/projects/${project.id}/scenarios/import?from=${source.id}`}
            aria-current={source.id === from ? "page" : undefined}
            className={cn(
              "-mb-px border-b-2 px-3 py-2 text-sm",
              source.id === from
                ? "border-brand font-medium"
                : "border-transparent text-muted-foreground hover:text-foreground",
            )}
          >
            {source.label}
          </Link>
        ))}
      </nav>

      {from === "openapi" ? (
        <OpenAPIImport projectId={project.id} />
      ) : (
        <PostmanImport
          projectId={project.id}
          environments={environments}
          canManageEnvironments={canManageEnvironments}
        />
      )}
    </div>
  );
}
