import Link from "next/link";

import { Button } from "@/components/ui/button";

import { runsHref, type Option, type RunFilters } from "../list";
import { runStatuses } from "../types";
import { runStatusLabel } from "./run-status-badge";

const field =
  "h-9 rounded-md border bg-background px-3 text-sm outline-none focus-visible:ring-2 focus-visible:ring-ring";

// RunFilters narrows the list by status, scenario and environment. A plain
// form that GETs the same address: the filters are in the URL, so a filtered
// list can be shared, bookmarked and walked back to, and it works without
// JavaScript. A new filter starts from the newest run: a cursor belongs to one
// list and is left behind.
export function RunFilterBar({
  projectId,
  filters,
  scenarios,
  environments,
}: {
  projectId: string;
  filters: RunFilters;
  scenarios: Option[];
  environments: Option[];
}) {
  const active = Boolean(filters.status || filters.scenario || filters.environment);

  return (
    <form
      method="get"
      action={`/projects/${projectId}/runs`}
      aria-label="Filter runs"
      className="flex flex-wrap items-end gap-3"
    >
      <label className="flex flex-col gap-1.5 text-xs text-muted-foreground">
        Status
        <select name="status" defaultValue={filters.status ?? ""} className={`${field} min-w-36 text-foreground`}>
          <option value="">All statuses</option>
          {runStatuses.map((status) => (
            <option key={status} value={status}>
              {runStatusLabel(status)}
            </option>
          ))}
        </select>
      </label>

      <label className="flex flex-col gap-1.5 text-xs text-muted-foreground">
        Scenario
        <select name="scenario" defaultValue={filters.scenario ?? ""} className={`${field} min-w-44 text-foreground`}>
          <option value="">All scenarios</option>
          {scenarios.map((s) => (
            <option key={s.id} value={s.id}>
              {s.name}
            </option>
          ))}
        </select>
      </label>

      <label className="flex flex-col gap-1.5 text-xs text-muted-foreground">
        Environment
        <select
          name="environment"
          defaultValue={filters.environment ?? ""}
          className={`${field} min-w-44 text-foreground`}
        >
          <option value="">All environments</option>
          {environments.map((e) => (
            <option key={e.id} value={e.id}>
              {e.name}
            </option>
          ))}
        </select>
      </label>

      <Button type="submit" variant="outline">
        Apply
      </Button>
      {active && (
        <Link href={runsHref(projectId, {})} className="pb-2 text-sm text-muted-foreground hover:text-foreground">
          Clear
        </Link>
      )}
    </form>
  );
}
