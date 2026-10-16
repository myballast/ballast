import { findAccess, findProject } from "@/features/projects/api";
import { getEnvironments } from "@/features/environments/api";
import { getRun } from "@/features/runs/api";
import { getScenarios } from "@/features/scenarios/api";
import { formatDateTime } from "@/lib/format";
import { can } from "@/lib/permissions";

import type { ShellScope } from "./types";

// loadProjectScope reads what the shell of a project shows: its name, the
// caller's role in it, and the names of what its address may point at.
//
// Null when there is no such project for the caller. The layout then draws the
// plain shell and lets the page answer 404 inside it.
export async function loadProjectScope(projectId: string): Promise<ShellScope | null> {
  const [project, access] = await Promise.all([findProject(projectId), findAccess(projectId)]);
  if (!project || !access) return null;

  // Without view_project there is nothing to read past the project's name, and
  // the lists would only be refused.
  const [scenarios, environments] = can(access, "view_project")
    ? await Promise.all([getScenarios(projectId), getEnvironments(projectId)])
    : [[], []];

  return {
    project: { id: project.id, name: project.name },
    role: access.role,
    names: {
      scenarios: Object.fromEntries(scenarios.map((s) => [s.id, s.name])),
      environments: Object.fromEntries(environments.map((e) => [e.id, e.name])),
    },
  };
}

// loadRunScope is the shell of a run's page. The address of a run carries no
// project, so the run is read to find it: a run knows its project.
//
// A run has no number of its own, so it is named by its scenario and the
// moment it was queued.
export async function loadRunScope(runId: string): Promise<ShellScope | null> {
  const run = await getRun(runId);
  if (!run) return null;

  const scope = await loadProjectScope(run.project_id);
  if (!scope) return null;

  const scenario = scope.names.scenarios[run.scenario_id] ?? "Run";
  return {
    ...scope,
    names: { ...scope.names, run: { id: run.id, label: `${scenario}, ${formatDateTime(run.created_at)}` } },
  };
}
