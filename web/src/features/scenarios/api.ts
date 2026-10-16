import { notFound } from "next/navigation";

import { ApiError, apiGet } from "@/lib/api-client";

import type { Scenario, ScenarioListItem, ScenarioSummary, ScenarioVersion, Threshold } from "./types";

function path(projectId: string, scenarioId?: string): string {
  const list = `/projects/${encodeURIComponent(projectId)}/scenarios`;
  return scenarioId ? `${list}/${encodeURIComponent(scenarioId)}` : list;
}

export async function getScenarios(projectId: string): Promise<ScenarioListItem[]> {
  const { scenarios } = await apiGet<{ scenarios: ScenarioListItem[] }>(path(projectId));
  return scenarios;
}

// getScenario reads a scenario as it is now, or null if it is gone.
export async function getScenario(projectId: string, scenarioId: string): Promise<ScenarioSummary | null> {
  return orNull(() => apiGet<ScenarioSummary>(path(projectId, scenarioId)));
}

// getScenarioDetail reads a scenario with its script and warnings, for its own
// page. No such scenario, or one of another project, is the 404 page.
export async function getScenarioDetail(projectId: string, scenarioId: string): Promise<Scenario> {
  try {
    return await apiGet<Scenario>(path(projectId, scenarioId));
  } catch (error) {
    if (error instanceof ApiError && (error.status === 404 || error.status === 400)) {
      notFound();
    }
    throw error;
  }
}

export async function getThresholds(projectId: string, scenarioId: string): Promise<Threshold[]> {
  const { thresholds } = await apiGet<{ thresholds: Threshold[] }>(`${path(projectId, scenarioId)}/thresholds`);
  return thresholds;
}

// getScenarioVersion reads one version of a scenario's script, or null if it
// cannot be found.
export async function getScenarioVersion(
  projectId: string,
  scenarioId: string,
  version: number,
): Promise<ScenarioVersion | null> {
  const page = await orNull(() => apiGet<{ versions: ScenarioVersion[] }>(`${path(projectId, scenarioId)}/versions`));
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
