"use server";

import { refresh, revalidatePath } from "next/cache";
import { redirect } from "next/navigation";

import { ApiError, apiDelete, apiPatch, apiPost } from "@/lib/api-client";
import { profileKinds, type ProfileKind, type ProfileParams } from "@/features/runs/types";

import type { Scenario, ScriptFinding, Threshold } from "./types";

function page(projectId: string, scenarioId?: string): string {
  const list = `/projects/${projectId}/scenarios`;
  return scenarioId ? `${list}/${scenarioId}` : list;
}

function path(projectId: string, scenarioId?: string): string {
  const list = `/projects/${encodeURIComponent(projectId)}/scenarios`;
  return scenarioId ? `${list}/${encodeURIComponent(scenarioId)}` : list;
}

function toLogin(next: string): never {
  redirect(`/login?next=${encodeURIComponent(next)}`);
}

// What the scenario form is told back: each problem next to the field it is
// about, and the script's findings line by line.
export type ScenarioFormState = {
  name?: string;
  params?: string;
  findings?: ScriptFinding[];
  error?: string;
  saved?: boolean;
};

type Fields = {
  name: string;
  // Left out for a scenario written as a graph: its script is generated, and
  // the API refuses one sent for it.
  script?: string;
  default_profile: ProfileKind;
  default_params?: ProfileParams;
};

function fields(form: FormData): Fields | ScenarioFormState {
  const name = String(form.get("name") ?? "").trim();
  const script = String(form.get("script") ?? "");
  const profile = String(form.get("profile") ?? "smoke") as ProfileKind;
  const generated = form.get("mode") === "graph";

  if (!name) return { name: "A scenario needs a name." };
  if (!generated && !script.trim()) return { findings: [{ code: "empty", message: "The script is empty." }] };
  if (!profileKinds.includes(profile)) return { params: "Pick a profile." };

  const result: Fields = { name, default_profile: profile };
  if (!generated) result.script = script;
  if (form.get("custom") === "on") {
    const whole = (field: string) => Number.parseInt(String(form.get(field) ?? ""), 10);
    const params = {
      target_vus: whole("target_vus"),
      ramp_up_s: whole("ramp_up_s"),
      plateau_s: whole("plateau_s"),
      ramp_down_s: whole("ramp_down_s"),
    };
    if (Object.values(params).some((value) => !Number.isFinite(value))) {
      return { params: "Every parameter must be a whole number." };
    }
    result.default_params = params;
  }
  return result;
}

// refusal puts what the API refused next to the field it is about.
function refusal(error: unknown, next: string): ScenarioFormState {
  if (!(error instanceof ApiError)) throw error;
  switch (error.status) {
    case 401:
      toLogin(next);
    case 403:
      return { error: "Writing scenarios takes the maintainer role on this project." };
    case 404:
      return { error: "This scenario no longer exists. Reload the page." };
    case 409:
      return { name: "Another scenario of this project already uses that name." };
    case 422: {
      const body = error.body as { findings?: ScriptFinding[] } | undefined;
      return { findings: body?.findings ?? [{ code: "refused", message: error.message }] };
    }
    case 400:
      // The API's 400s say which rule was broken; the prefix says which field.
      return error.message.startsWith("invalid profile parameters")
        ? { params: error.message }
        : { name: error.message };
  }
  throw error;
}

// createScenario stores a scenario and its first version, and takes the browser
// to it — where its thresholds are set. A refused script is not stored.
export async function createScenario(
  projectId: string,
  _previous: ScenarioFormState,
  form: FormData,
): Promise<ScenarioFormState> {
  const body = fields(form);
  if (!("default_profile" in body)) return body;

  let id: string;
  try {
    ({ id } = await apiPost<Scenario>(path(projectId), body));
  } catch (error) {
    return refusal(error, `${page(projectId)}/new`);
  }

  // The shell around the project names what exists, and it is drawn once per
  // visit to the project: it has to hear about this before the browser lands.
  revalidatePath(`/projects/${projectId}`, "layout");
  redirect(page(projectId, id));
}

// updateScenario saves the form. A new script, profile or set of parameters
// writes the next version; a new name does not. Either way, runs already
// recorded keep the version they executed.
export async function updateScenario(
  projectId: string,
  scenarioId: string,
  _previous: ScenarioFormState,
  form: FormData,
): Promise<ScenarioFormState> {
  const body = fields(form);
  if (!("default_profile" in body)) return body;

  try {
    await apiPatch<Scenario>(path(projectId, scenarioId), body);
  } catch (error) {
    return refusal(error, page(projectId, scenarioId));
  }

  refresh();
  return { saved: true };
}

export type ThresholdState = { error?: string; done?: boolean };

function thresholdRefusal(error: unknown, next: string): ThresholdState {
  if (!(error instanceof ApiError)) throw error;
  switch (error.status) {
    case 401:
      toLogin(next);
    case 403:
      return { error: "Changing thresholds takes the maintainer role on this project." };
    case 404:
      return { error: "This threshold no longer exists. Reload the page." };
    case 409:
      return { error: "This scenario already has exactly that threshold." };
    case 400:
      return { error: error.message };
  }
  throw error;
}

function rule(form: FormData): { metric: string; expression: string } | ThresholdState {
  const metric = String(form.get("metric") ?? "").trim();
  const expression = String(form.get("expression") ?? "").trim();
  if (!metric || !expression) return { error: "A threshold needs a metric and an expression." };
  return { metric, expression };
}

export async function addThreshold(
  projectId: string,
  scenarioId: string,
  _previous: ThresholdState,
  form: FormData,
): Promise<ThresholdState> {
  const body = rule(form);
  if (!("metric" in body)) return body;

  try {
    await apiPost<Threshold>(`${path(projectId, scenarioId)}/thresholds`, body);
  } catch (error) {
    return thresholdRefusal(error, page(projectId, scenarioId));
  }

  refresh();
  return { done: true };
}

export async function updateThreshold(
  projectId: string,
  scenarioId: string,
  thresholdId: string,
  _previous: ThresholdState,
  form: FormData,
): Promise<ThresholdState> {
  const body = rule(form);
  if (!("metric" in body)) return body;

  try {
    await apiPatch<Threshold>(`${path(projectId, scenarioId)}/thresholds/${encodeURIComponent(thresholdId)}`, body);
  } catch (error) {
    return thresholdRefusal(error, page(projectId, scenarioId));
  }

  refresh();
  return { done: true };
}

export async function deleteThreshold(
  projectId: string,
  scenarioId: string,
  thresholdId: string,
): Promise<ThresholdState> {
  try {
    await apiDelete(`${path(projectId, scenarioId)}/thresholds/${encodeURIComponent(thresholdId)}`);
  } catch (error) {
    return thresholdRefusal(error, page(projectId, scenarioId));
  }

  refresh();
  return { done: true };
}
