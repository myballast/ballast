"use server";

import { refresh } from "next/cache";
import { redirect } from "next/navigation";

import { ApiError, apiPatch, apiPost } from "@/lib/api-client";
import type { ProfileKind, ProfileParams } from "@/features/runs/types";
import type { GraphReport, Scenario, ScenarioGraph } from "@/features/scenarios/types";

export type GraphState = { ok?: boolean; error?: string };

function path(projectId: string, scenarioId: string): string {
  return `/projects/${encodeURIComponent(projectId)}/scenarios/${encodeURIComponent(scenarioId)}`;
}

// What the API refused, in a sentence. The 422 and 409 messages say which rule
// the graph broke and are written for whoever sent it.
function refusal(error: unknown, projectId: string, scenarioId: string): GraphState {
  if (!(error instanceof ApiError)) throw error;
  switch (error.status) {
    case 401:
      redirect(`/login?next=${encodeURIComponent(`/projects/${projectId}/scenarios/${scenarioId}/builder`)}`);
    case 403:
      return { error: "Changing a scenario takes the maintainer role on this project." };
    case 404:
      return { error: "This scenario no longer exists. Reload the page." };
    case 409:
    case 422:
    case 400:
      return { error: error.message };
  }
  throw error;
}

// saveGraph writes the graph as the scenario's next version. The script is
// generated from it by the API; nothing here sends one.
export async function saveGraph(projectId: string, scenarioId: string, graph: ScenarioGraph): Promise<GraphState> {
  try {
    await apiPatch<Scenario>(path(projectId, scenarioId), { graph });
  } catch (error) {
    return refusal(error, projectId, scenarioId);
  }

  refresh();
  return { ok: true };
}

export type ValidateState = { report?: GraphReport; error?: string };

// validateGraph looks the graph over without saving it: everything wrong with it,
// each fault on its node, or the script a save would store. A graph that is wrong is
// an answer and not an error, so what comes back is a report either way.
export async function validateGraph(projectId: string, scenarioId: string, graph: ScenarioGraph): Promise<ValidateState> {
  try {
    const report = await apiPost<GraphReport>(`${path(projectId, scenarioId)}/graph/validate`, { graph });
    return { report };
  } catch (error) {
    return { error: refusal(error, projectId, scenarioId).error };
  }
}

export type ProfileState = { ok?: boolean; error?: string };

// saveProfile writes the scenario's default profile and its parameters. As with the
// form of the scenario page, that is the next version of the scenario — the script
// and the graph it holds do not change — and runs already recorded keep the version
// they executed.
export async function saveProfile(
  projectId: string,
  scenarioId: string,
  profile: ProfileKind,
  params: ProfileParams,
): Promise<ProfileState> {
  try {
    await apiPatch<Scenario>(path(projectId, scenarioId), { default_profile: profile, default_params: params });
  } catch (error) {
    // The 400 of an invalid profile says which rule was broken, and is written for
    // whoever sent it.
    const refused = refusal(error, projectId, scenarioId);
    return { error: refused.error };
  }

  refresh();
  return { ok: true };
}
