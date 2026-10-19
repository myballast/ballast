import type { RunStatus } from "@/features/runs/types";

import type { ProjectOverview } from "./types";

export type SortKey = "recent" | "name" | "status";
export type ViewMode = "grid" | "list";

export const sortLabels: Record<SortKey, string> = {
  recent: "Recent activity",
  name: "Name",
  status: "Last status",
};

export function isSortKey(value: unknown): value is SortKey {
  return value === "recent" || value === "name" || value === "status";
}

export function isViewMode(value: unknown): value is ViewMode {
  return value === "grid" || value === "list";
}

// What asks for attention comes first: a project that failed is what somebody
// opened the list to find, and one that never ran has nothing to say yet.
const attention: Record<RunStatus, number> = {
  failed: 0,
  errored: 1,
  timeout: 2,
  orphaned: 3,
  running: 4,
  queued: 5,
  passed: 6,
  cancelled: 7,
  rejected: 8,
  skipped: 9,
};

const neverRan = 99;

// When a project last did anything: its last run, ended or just started, and when
// it was created for one that never ran. A project nobody has touched still has
// a place in the order.
export function lastActivity(overview: ProjectOverview): number {
  if (overview.access === "full" && overview.lastRun) {
    return Date.parse(overview.lastRun.ended_at ?? overview.lastRun.created_at);
  }
  return Date.parse(overview.project.created_at);
}

function statusRank(overview: ProjectOverview): number {
  if (overview.access !== "full" || !overview.lastRun) return neverRan;
  return attention[overview.lastRun.status];
}

const byName = (a: ProjectOverview, b: ProjectOverview) =>
  a.project.name.localeCompare(b.project.name, "en", { sensitivity: "base" }) ||
  a.project.id.localeCompare(b.project.id);

const orders: Record<SortKey, (a: ProjectOverview, b: ProjectOverview) => number> = {
  recent: (a, b) => lastActivity(b) - lastActivity(a) || byName(a, b),
  name: byName,
  status: (a, b) => statusRank(a) - statusRank(b) || lastActivity(b) - lastActivity(a) || byName(a, b),
};

// arrange puts the projects in order, favourites first whatever the order is: a
// star says "keep this where I can see it", and a sort that buried it would be
// undoing what it was asked for. Within each of the two groups the chosen order
// holds. The input is not changed.
export function arrange(overviews: ProjectOverview[], sort: SortKey): ProjectOverview[] {
  return [...overviews].sort(
    (a, b) => Number(b.project.favorite) - Number(a.project.favorite) || orders[sort](a, b),
  );
}

// The "across <m> infrastructures" of the subtitle: how many different ones the
// projects name. Projects that say nothing are not an infrastructure of their
// own, and the same name in another case is the same infrastructure.
export function infrastructuresOf(overviews: ProjectOverview[]): number {
  const named = overviews.map((o) => o.project.infra.trim().toLowerCase()).filter((infra) => infra !== "");
  return new Set(named).size;
}

// subtitle is the line under "Projects": "6 projects across 4 infrastructures",
// and without the second half when no project says where it runs.
export function subtitle(overviews: ProjectOverview[]): string {
  const projects = overviews.length;
  const infrastructures = infrastructuresOf(overviews);

  const count = `${projects} ${projects === 1 ? "project" : "projects"}`;
  if (infrastructures === 0) return count;
  return `${count} across ${infrastructures} ${infrastructures === 1 ? "infrastructure" : "infrastructures"}`;
}

// place is "k8s · eu-west-1", or whichever half is known, or nothing.
export function place(project: { infra: string; region: string }): string {
  return [project.infra.trim(), project.region.trim()].filter((part) => part !== "").join(" · ");
}

// parseStack reads what somebody typed into the stack field — labels separated by
// commas or spaces — into the list the API takes. It does not validate: what a
// label may hold is the API's to say, and its refusal names the label.
export function parseStack(text: string): string[] {
  return text
    .split(/[\s,]+/)
    .map((label) => label.trim())
    .filter((label) => label !== "");
}
