import { ApiError, apiGet } from "@/lib/api-client";

import type { ScenarioSummary, ScenarioVersion } from "./types";

export async function getScenarios(projectId: string): Promise<ScenarioSummary[]> {
  const { scenarios } = await apiGet<{ scenarios: ScenarioSummary[] }>(
    `/projects/${encodeURIComponent(projectId)}/scenarios`,
  );
  return scenarios;
}

// getScenario reads a scenario as it is now, or null if it is gone.
export async function getScenario(projectId: string, scenarioId: string): Promise<ScenarioSummary | null> {
  return orNull(() =>
    apiGet<ScenarioSummary>(`/projects/${encodeURIComponent(projectId)}/scenarios/${encodeURIComponent(scenarioId)}`),
  );
}

// getScenarioVersion reads one version of a scenario's script, or null if it
// cannot be found.
export async function getScenarioVersion(
  projectId: string,
  scenarioId: string,
  version: number,
): Promise<ScenarioVersion | null> {
  const page = await orNull(() =>
    apiGet<{ versions: ScenarioVersion[] }>(
      `/projects/${encodeURIComponent(projectId)}/scenarios/${encodeURIComponent(scenarioId)}/versions`,
    ),
  );
  return page?.versions.find((v) => v.version === version) ?? null;
}

async function orNull<T>(read: () => Promise<T>): Promise<T | null> {
  try {
    return await read();
  } catch (error) {
    if (error instanceof ApiError && error.status === 404) return null;
    throw error;
  }
}
