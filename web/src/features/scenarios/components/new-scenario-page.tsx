import Link from "next/link";

import type { CatalogEndpoint } from "@/features/endpoints/types";
import type { Project } from "@/features/projects/types";
import { cn } from "@/lib/utils";

import { ScenarioForm } from "./scenario-form";

export type Start = "code" | "graph";

const starts: { id: Start; label: string; text: string }[] = [
  { id: "code", label: "Code", text: "A k6 script you write, from a starter that already passes the checks." },
  { id: "graph", label: "Graph", text: "Requests, conditions and loops drawn on a canvas. Ballast writes the script." },
];

// NewScenarioPage frames the creation form. The thresholds come after: they
// belong to a scenario, which exists once this is saved.
//
// A scenario is written as code or as a graph, and there is no way from one to the
// other, so the choice is made here, first, and said in words.
export function NewScenarioPage({
  project,
  endpoints,
  start,
}: {
  project: Project;
  endpoints: CatalogEndpoint[];
  start: Start;
}) {
  return (
    <div className="flex flex-col gap-8">
      <header className="flex flex-col gap-1">
        <Link href={`/projects/${project.id}/scenarios`} className="text-xs text-muted-foreground hover:underline">
          {project.name} · Scenarios
        </Link>
        <h1 className="text-3xl font-normal tracking-tight">New scenario</h1>
        <p className="max-w-xl text-sm text-muted-foreground">
          Once it is saved, you can add its thresholds. How it is written cannot change afterwards.
        </p>
      </header>

      <nav aria-label="How the scenario is written" className="grid max-w-xl gap-2 sm:grid-cols-2">
        {starts.map((option) => (
          <Link
            key={option.id}
            href={`/projects/${project.id}/scenarios/new?mode=${option.id}`}
            aria-current={start === option.id ? "page" : undefined}
            className={cn(
              "flex flex-col gap-1 rounded-xl border px-3 py-2.5 text-sm text-muted-foreground hover:bg-muted",
              start === option.id && "border-brand text-foreground",
            )}
          >
            <span className="font-medium">{option.label}</span>
            <span className="text-xs">{option.text}</span>
          </Link>
        ))}
      </nav>

      {/* Keyed on the choice: switching starts the form again, as its fields differ. */}
      <ScenarioForm key={start} projectId={project.id} endpoints={endpoints} startAs={start} />
    </div>
  );
}
