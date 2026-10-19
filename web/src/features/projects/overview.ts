import { getEnvironments } from "@/features/environments/api";
import { getProjectRuns } from "@/features/runs/api";
import { runRows } from "@/features/runs/list";
import { getScenarios } from "@/features/scenarios/api";
import { can } from "@/lib/permissions";

import { getAccess, getProject } from "./api";
import { getProjectStats } from "./stats";

// How many runs the project's page lists. The rest are one click away.
const latest = 10;

// loadProjectPage reads everything the project page shows, at once. Without
// view_project there is nothing to read past the project's name, and nothing
// is asked for.
export async function loadProjectPage(projectId: string) {
  const [project, access] = await Promise.all([getProject(projectId), getAccess(projectId)]);
  if (!can(access, "view_project")) {
    return { project, access, restricted: true as const };
  }

  const [scenarios, environments, stats, page] = await Promise.all([
    getScenarios(projectId),
    getEnvironments(projectId),
    getProjectStats(projectId),
    getProjectRuns(projectId, { limit: latest }),
  ]);

  const names = (list: { id: string; name: string }[]) => list.map(({ id, name }) => ({ id, name }));
  const rows = runRows(page.runs, names(scenarios), names(environments));

  return { project, access, restricted: false as const, scenarios, environments, stats, rows };
}
