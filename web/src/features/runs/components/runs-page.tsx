import { ChevronLeft, ChevronRight, Play } from "lucide-react";
import Link from "next/link";

import { EmptyState } from "@/components/states";
import { buttonVariants } from "@/components/ui/button";
import type { Project } from "@/features/projects/types";
import { can, type Access } from "@/lib/permissions";

import { pageSize, runsHref, type RunsPage as Page } from "../list";
import { RunFilterBar } from "./run-filters";
import { RunsTable } from "./runs-table";

// RunsPage is a project's runs: a table of them, narrowed by the filters,
// a page of twenty-five at a time, newest first.
export function RunsPage({ project, access, page }: { project: Project; access: Access; page: Page }) {
  const { filters, rows, next, prev, inHistory } = page;
  const filtered = Boolean(filters.status || filters.scenario || filters.environment);
  const launch = can(access, "run_test");

  return (
    <div className="flex flex-col gap-8">
      <header className="flex flex-wrap items-start justify-between gap-4">
        <div className="flex flex-col gap-1">
          <Link href={`/projects/${project.id}`} className="text-xs text-muted-foreground hover:underline">
            {project.name}
          </Link>
          <h1 className="text-3xl font-normal tracking-tight">Runs</h1>
        </div>
        {/* Hidden from whoever cannot launch; the API refuses them anyway. */}
        {launch && (
          <Link href={`/projects/${project.id}/runs/new`} className={buttonVariants()}>
            <Play aria-hidden />
            Run a test
          </Link>
        )}
      </header>

      {/* The filters stay when a filter leaves nothing, so that it can be
          changed; they are only left out of a project that has no run at all. */}
      {(rows.length > 0 || filtered || inHistory) && (
        <RunFilterBar
          projectId={project.id}
          filters={filters}
          scenarios={page.scenarios}
          environments={page.environments}
        />
      )}

      {rows.length === 0 ? (
        filtered ? (
          <EmptyState
            title="No run matches these filters"
            description="Change a filter, or clear them to see every run of the project."
            action={
              <Link href={runsHref(project.id, {})} className={buttonVariants({ variant: "outline" })}>
                Clear filters
              </Link>
            }
          />
        ) : (
          <EmptyState
            icon={Play}
            title="No run yet"
            description="Runs appear here once a scenario has been launched against a verified environment."
            action={
              launch ? (
                <Link href={`/projects/${project.id}/runs/new`} className={buttonVariants()}>
                  <Play aria-hidden />
                  Run a test
                </Link>
              ) : undefined
            }
          />
        )
      ) : (
        <div className="flex flex-col gap-3">
          <RunsTable rows={rows} />
          <nav aria-label="Pages of runs" className="flex items-center justify-between gap-3 text-sm">
            <span className="text-xs text-muted-foreground">
              Up to <span className="num">{pageSize}</span> runs a page, newest first
            </span>
            <span className="flex items-center gap-2">
              {prev && (
                <Link
                  href={runsHref(project.id, filters, { after: prev })}
                  className={buttonVariants({ variant: "outline", size: "sm" })}
                >
                  <ChevronLeft aria-hidden />
                  Newer
                </Link>
              )}
              {next && (
                <Link
                  href={runsHref(project.id, filters, { before: next })}
                  className={buttonVariants({ variant: "outline", size: "sm" })}
                >
                  Older
                  <ChevronRight aria-hidden />
                </Link>
              )}
            </span>
          </nav>
        </div>
      )}
    </div>
  );
}
