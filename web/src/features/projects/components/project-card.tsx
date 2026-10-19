import { Lock } from "lucide-react";
import Link from "next/link";

import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";

import { place } from "../arrangement";
import type { ProjectOverview } from "../types";
import { FavoriteButton } from "./favorite-button";
import { RunLine } from "./run-line";
import { StackChips } from "./stack-chips";

// ProjectCard is one project of the list: its name and slug, where it runs, what
// it is made of, and how its last run ended.
//
// Everything about a project that was never written is left out: no empty chip,
// no "undefined", no blank where the infrastructure would be.
export function ProjectCard({ overview, now }: { overview: ProjectOverview; now: Date }) {
  const { project } = overview;
  const where = place(project);

  return (
    <Card>
      <CardHeader>
        <div className="flex items-center gap-2">
          <CardTitle className="min-w-0 truncate">
            <Link href={`/projects/${project.id}`} className="hover:underline">
              {project.name}
            </Link>
          </CardTitle>
          {where && (
            <span className="data shrink-0 rounded-md border px-1.5 py-px text-[10.5px] whitespace-nowrap text-muted-foreground">
              {where}
            </span>
          )}
          <span className="ml-auto">
            <FavoriteButton projectId={project.id} name={project.name} favorite={project.favorite} />
          </span>
        </div>
        <p className="data truncate text-xs text-muted-foreground">{project.slug}</p>
      </CardHeader>

      <CardContent className="flex flex-col gap-3 text-sm">
        <StackChips stack={project.stack} />
        {overview.access === "restricted" ? (
          <p className="flex items-center gap-2 text-muted-foreground">
            <Lock aria-hidden className="size-4 shrink-0" />
            Your role in this project does not include its details.
          </p>
        ) : (
          <RunLine environments={overview.environments.length} run={overview.lastRun} now={now} />
        )}
      </CardContent>
    </Card>
  );
}
