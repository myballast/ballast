import { Lock } from "lucide-react";
import Link from "next/link";

import { place } from "../arrangement";
import type { ProjectOverview } from "../types";
import { FavoriteButton } from "./favorite-button";
import { RunLine } from "./run-line";
import { StackChips } from "./stack-chips";

const columns =
  "grid grid-cols-[2.25rem_minmax(0,1.3fr)_minmax(0,1fr)_minmax(0,1.2fr)_minmax(0,1.6fr)] items-center gap-3 px-4";

// ProjectTable is the list view: the same facts as the cards, one project to a
// line. Favourites are first here as in the grid, because the order is decided
// before either is drawn.
export function ProjectTable({ overviews, now }: { overviews: ProjectOverview[]; now: Date }) {
  return (
    <div className="relative overflow-x-auto rounded-xl border bg-card">
      <div role="table" aria-label="Projects" className="min-w-[52rem]">
        <div role="row" className={`${columns} border-b py-2 text-[11px] tracking-wider text-muted-foreground uppercase`}>
          <div role="columnheader">
            <span className="sr-only">Favorite</span>
          </div>
          <div role="columnheader">Project</div>
          <div role="columnheader">Infrastructure</div>
          <div role="columnheader">Stack</div>
          <div role="columnheader">Last run</div>
        </div>

        {overviews.map((overview) => {
          const { project } = overview;
          const where = place(project);

          return (
            <div key={project.id} role="row" className={`${columns} min-h-12 border-b py-2 text-sm last:border-b-0 hover:bg-muted/30`}>
              <div role="cell">
                <FavoriteButton projectId={project.id} name={project.name} favorite={project.favorite} />
              </div>
              <div role="cell" className="min-w-0">
                <Link href={`/projects/${project.id}`} className="block truncate text-[13px] hover:underline">
                  {project.name}
                </Link>
                <span className="data block truncate text-[11px] text-muted-foreground">
                  {project.slug}
                  {project.owner && <> · {project.owner}</>}
                </span>
              </div>
              <div role="cell" className="data truncate text-[11.5px] text-muted-foreground">
                {where}
              </div>
              <div role="cell">
                <StackChips stack={project.stack} />
              </div>
              <div role="cell">
                {overview.access === "restricted" ? (
                  <span className="flex items-center gap-2 text-xs text-muted-foreground">
                    <Lock aria-hidden className="size-3.5 shrink-0" />
                    Details are restricted
                  </span>
                ) : (
                  <RunLine environments={overview.environments.length} run={overview.lastRun} now={now} />
                )}
              </div>
            </div>
          );
        })}
      </div>
    </div>
  );
}
