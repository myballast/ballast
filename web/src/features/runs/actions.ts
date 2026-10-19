"use server";

import { refresh } from "next/cache";
import { redirect } from "next/navigation";

import { ApiError, apiDelete, apiPost } from "@/lib/api-client";
import { getScenarioDetail } from "@/features/scenarios/api";

import type { ProfileKind, ProfileParams } from "./types";

export type LaunchState = { error?: string };

// launchRun queues a run and sends the browser to it. Every rule that matters
// — a verified target, the caps, the allowed hours, the permission — is the
// API's to apply; this only turns its refusals into sentences.
export async function launchRun(projectId: string, _previous: LaunchState, form: FormData): Promise<LaunchState> {
  const scenarioId = String(form.get("scenario_id") ?? "");
  const environmentId = String(form.get("environment_id") ?? "");
  const profile = String(form.get("profile") ?? "") as ProfileKind;

  if (!scenarioId || !environmentId || !profile) {
    return { error: "Pick a scenario, an environment and a profile." };
  }

  const body: {
    scenario_id: string;
    environment_id: string;
    profile: ProfileKind;
    params?: ProfileParams;
  } = { scenario_id: scenarioId, environment_id: environmentId, profile };

  // Parameters are sent only when the user chose their own. Left out, the
  // API applies the profile's template.
  if (form.get("custom") === "on") {
    const number = (name: string) => Number.parseInt(String(form.get(name) ?? ""), 10);
    body.params = {
      target_vus: number("target_vus"),
      ramp_up_s: number("ramp_up_s"),
      plateau_s: number("plateau_s"),
      ramp_down_s: number("ramp_down_s"),
    };
    if (Object.values(body.params).some((value) => !Number.isFinite(value))) {
      return { error: "Every parameter must be a whole number." };
    }
  }

  let id: string;
  try {
    ({ id } = await apiPost<{ id: string; status: string }>("/runs", body));
  } catch (error) {
    if (!(error instanceof ApiError)) throw error;
    switch (error.status) {
      case 401:
        redirect(`/login?next=${encodeURIComponent(`/projects/${projectId}/runs/new`)}`);
      case 404:
        return { error: "That scenario or environment no longer exists. Reload the page." };
      case 400:
      case 403:
      case 503:
        // The API's messages for these say exactly why, and are written for
        // whoever sent the request.
        return { error: error.message };
    }
    throw error;
  }

  redirect(`/runs/${id}`);
}

// launchDefaultRun is the "Run" of a scenario's own page: it queues a run of the
// saved scenario with its default profile and parameters, against the environment
// chosen in the bar, and takes the browser to the run.
//
// The profile and parameters are read here, from the scenario as it is stored, and
// not taken from the page: what is run is the saved version, and a page that held
// an unsaved edit has no say in it. A scenario with no parameters of its own —
// stored with every one at zero — leaves them out, and the API applies the
// profile's template. Every rule that matters, a verified target above all, is the
// API's to apply.
export async function launchDefaultRun(
  projectId: string,
  scenarioId: string,
  environmentId: string,
): Promise<LaunchState> {
  if (!environmentId) return { error: "Pick an environment to run against." };

  const scenario = await getScenarioDetail(projectId, scenarioId);
  const own = scenario.default_params.target_vus >= 1;

  let id: string;
  try {
    ({ id } = await apiPost<{ id: string; status: string }>("/runs", {
      scenario_id: scenarioId,
      environment_id: environmentId,
      profile: scenario.default_profile,
      ...(own ? { params: scenario.default_params } : {}),
    }));
  } catch (error) {
    if (!(error instanceof ApiError)) throw error;
    switch (error.status) {
      case 401:
        redirect(`/login?next=${encodeURIComponent(`/projects/${projectId}/scenarios/${scenarioId}`)}`);
      case 404:
        return { error: "That scenario or environment no longer exists. Reload the page." };
      case 400:
      case 403:
      case 429:
      case 503:
        // The API's messages for these say exactly why, and are written for
        // whoever sent the request.
        return { error: error.message };
    }
    throw error;
  }

  redirect(`/runs/${id}`);
}

// rerunRun queues a run again with what the run froze — the scenario at the same
// version, the profile and the load it had — and takes the browser to it. What
// the scenario is today does not come into it; what the environment accepts
// today does, and the API says so when it refuses or lowers the load.
export async function rerunRun(runId: string): Promise<{ error?: string }> {
  let id: string;
  try {
    ({ id } = await apiPost<{ id: string; status: string }>(`/runs/${encodeURIComponent(runId)}/rerun`, undefined));
  } catch (error) {
    if (!(error instanceof ApiError)) throw error;
    switch (error.status) {
      case 401:
        redirect(`/login?next=${encodeURIComponent(`/runs/${runId}`)}`);
      case 404:
        return { error: "This run no longer exists. Reload the page." };
      case 400:
      case 403:
      case 429:
      case 503:
        // The API's messages for these say exactly why, and are written for
        // whoever sent the request.
        return { error: error.message };
    }
    throw error;
  }

  redirect(`/runs/${id}`);
}

export type CancelState = { error?: string; outcome?: "cancelled" | "stopping" };

// cancelRun stops a queued run, or asks a running one to stop. The API answers
// differently for the two, because the facts differ: a queued run is cancelled
// when this returns, a running one only once its worker has stopped the load.
export async function cancelRun(runId: string): Promise<CancelState> {
  let outcome: string;
  try {
    ({ outcome } = await apiDelete<{ id: string; status: string; outcome: string }>(
      `/runs/${encodeURIComponent(runId)}`,
    ));
  } catch (error) {
    if (!(error instanceof ApiError)) throw error;
    switch (error.status) {
      case 401:
        redirect(`/login?next=${encodeURIComponent(`/runs/${runId}`)}`);
      case 409:
        refresh();
        return { error: "This run has already finished; there is nothing left to stop." };
      case 403:
      case 404:
        return { error: "You cannot stop this run." };
    }
    throw error;
  }

  refresh();
  return { outcome: outcome === "cancelled" ? "cancelled" : "stopping" };
}
