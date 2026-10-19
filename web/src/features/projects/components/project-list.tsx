import { FolderKanban } from "lucide-react";

import { EmptyState } from "@/components/states";

import { arrange, subtitle, type SortKey, type ViewMode } from "../arrangement";
import type { ProjectOverview } from "../types";
import { CreateProject } from "./create-project";
import { ProjectCard } from "./project-card";
import { ProjectTable } from "./project-table";
import { ProjectToolbar } from "./project-toolbar";

// ProjectList is the workspace's front page: how many projects there are and on
// how many infrastructures, the order and view the person chose, and the
// projects, favourites first.
export function ProjectList({
  overviews,
  now,
  sort = "recent",
  view = "grid",
}: {
  overviews: ProjectOverview[];
  now: Date;
  sort?: SortKey;
  view?: ViewMode;
}) {
  const ordered = arrange(overviews, sort);

  return (
    <div className="flex flex-col gap-6">
      <div className="flex flex-wrap items-start justify-between gap-4">
        <div className="flex flex-col gap-1">
          <h1 className="text-3xl font-normal tracking-tight">Projects</h1>
          <p className="text-sm text-muted-foreground">{subtitle(overviews)}</p>
        </div>
        <div className="flex flex-wrap items-center gap-3">
          {overviews.length > 0 && <ProjectToolbar sort={sort} view={view} />}
          <CreateProject />
        </div>
      </div>

      {overviews.length === 0 ? (
        <EmptyState
          icon={FolderKanban}
          title="No project yet"
          description="A project holds scenarios, the environments they run against, and their runs. Create one to start."
        />
      ) : view === "list" ? (
        <ProjectTable overviews={ordered} now={now} />
      ) : (
        <ul className="grid grid-cols-1 gap-3.5 md:grid-cols-2 xl:grid-cols-3">
          {ordered.map((overview) => (
            <li key={overview.project.id}>
              <ProjectCard overview={overview} now={now} />
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
