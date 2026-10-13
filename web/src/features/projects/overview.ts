import { getEnvironments } from "@/features/environments/api";
import { historyPage } from "@/features/runs/history";
import { getScenarios } from "@/features/scenarios/api";
import { can } from "@/lib/permissions";

import { getAccess, getProject } from "./api";

// loadProjectPage reads everything the project page shows, at once. Without
// view_project there is nothing to read past the project's name, and nothing
// is asked for.
export async function loadProjectPage(projectId: string) {
  const [project, access] = await Promise.all([getProject(projectId), getAccess(projectId)]);
  if (!can(access, "view_project")) {
    return { project, access, restricted: true as const };
  }

  const [scenarios, environments] = await Promise.all([getScenarios(projectId), getEnvironments(projectId)]);
  const history = await historyPage(projectId, {
    scenarios: Object.fromEntries(scenarios.map((s) => [s.id, s.name])),
    environments: Object.fromEntries(environments.map((e) => [e.id, e.name])),
  });

  return { project, access, restricted: false as const, scenarios, environments, history };
}
