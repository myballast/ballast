"use server";

import { redirect } from "next/navigation";

import { ApiError, apiPost } from "@/lib/api-client";

import { historyPage, type HistoryPage, type Names } from "./history";
import type { ProfileKind, ProfileParams } from "./types";

// loadOlderRuns is the history's "Load older runs". The API checks the
// caller's permission on the project for this page as for the first.
export async function loadOlderRuns(projectId: string, names: Names, before: string): Promise<HistoryPage> {
  return historyPage(projectId, names, before);
}

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
