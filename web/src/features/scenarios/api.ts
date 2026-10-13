import { apiGet } from "@/lib/api-client";

import type { ScenarioSummary } from "./types";

export async function getScenarios(projectId: string): Promise<ScenarioSummary[]> {
  const { scenarios } = await apiGet<{ scenarios: ScenarioSummary[] }>(
    `/projects/${encodeURIComponent(projectId)}/scenarios`,
  );
  return scenarios;
}
