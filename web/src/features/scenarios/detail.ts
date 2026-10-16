import { getAccess, getProject } from "@/features/projects/api";
import type { Project } from "@/features/projects/types";
import type { Access } from "@/lib/permissions";

import { getScenarioDetail, getThresholds } from "./api";
import type { Scenario, Threshold } from "./types";

export type ScenarioPage = {
  project: Project;
  access: Access;
  scenario: Scenario;
  thresholds: Threshold[];
};

// loadScenarioPage reads everything the scenario page shows, at once.
export async function loadScenarioPage(projectId: string, scenarioId: string): Promise<ScenarioPage> {
  const [project, access, scenario, thresholds] = await Promise.all([
    getProject(projectId),
    getAccess(projectId),
    getScenarioDetail(projectId, scenarioId),
    getThresholds(projectId, scenarioId),
  ]);
  return { project, access, scenario, thresholds };
}
