import { getCatalogOrNothing } from "@/features/endpoints/api";
import type { CatalogEndpoint } from "@/features/endpoints/types";
import { getEnvironments } from "@/features/environments/api";
import type { Environment } from "@/features/environments/types";
import { getAccess, getProject } from "@/features/projects/api";
import type { Project } from "@/features/projects/types";
import { can, type Access } from "@/lib/permissions";

import { getScenarioDetail, getThresholds } from "./api";
import type { Scenario, Threshold } from "./types";

export type ScenarioPage = {
  project: Project;
  access: Access;
  scenario: Scenario;
  thresholds: Threshold[];
  // For the bar's target selector. Empty for somebody who cannot launch runs:
  // not asked for, rather than asked for and refused.
  environments: Environment[];
  // The project's catalog, for the script editor. Empty for somebody who cannot
  // write scenarios, who has no editor to offer it in.
  endpoints: CatalogEndpoint[];
};

// loadScenarioPage reads everything the scenario page shows, at once.
export async function loadScenarioPage(projectId: string, scenarioId: string): Promise<ScenarioPage> {
  const [project, access, scenario, thresholds] = await Promise.all([
    getProject(projectId),
    getAccess(projectId),
    getScenarioDetail(projectId, scenarioId),
    getThresholds(projectId, scenarioId),
  ]);
  const environments = can(access, "run_test") ? await getEnvironments(projectId) : [];
  const endpoints = can(access, "manage_scenarios") ? await getCatalogOrNothing(projectId) : [];
  return { project, access, scenario, thresholds, environments, endpoints };
}
