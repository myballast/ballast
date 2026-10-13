import { FolderKanban } from "lucide-react";

import { EmptyState } from "@/components/states";

import type { ProjectOverview } from "../types";
import { CreateProject } from "./create-project";
import { ProjectCard } from "./project-card";

export function ProjectList({ overviews, now }: { overviews: ProjectOverview[]; now: Date }) {
  return (
    <div className="flex flex-col gap-6">
      <div className="flex flex-wrap items-start justify-between gap-4">
        <h1 className="text-3xl font-normal tracking-tight">Projects</h1>
        <CreateProject />
      </div>

      {overviews.length === 0 ? (
        <EmptyState
          icon={FolderKanban}
          title="No project yet"
          description="A project holds scenarios, the environments they run against, and their runs. Create one to start."
        />
      ) : (
        <ul className="grid grid-cols-1 gap-3.5 md:grid-cols-2 xl:grid-cols-3">
          {overviews.map((overview) => (
            <li key={overview.project.id}>
              <ProjectCard overview={overview} now={now} />
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
