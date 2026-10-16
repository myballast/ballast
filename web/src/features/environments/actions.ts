"use server";

import { refresh, revalidatePath } from "next/cache";
import { redirect } from "next/navigation";

import { ApiError, apiDelete, apiPatch, apiPost, apiPut } from "@/lib/api-client";

import type { AllowedWindow, Environment } from "./types";

function page(projectId: string, environmentId?: string): string {
  const list = `/projects/${projectId}/environments`;
  return environmentId ? `${list}/${environmentId}` : list;
}

function path(projectId: string, environmentId?: string): string {
  const list = `/projects/${encodeURIComponent(projectId)}/environments`;
  return environmentId ? `${list}/${encodeURIComponent(environmentId)}` : list;
}

function toLogin(next: string): never {
  redirect(`/login?next=${encodeURIComponent(next)}`);
}

export type FormState = { error?: string };

type Fields = {
  name: string;
  base_url: string;
  max_vus: number;
  max_duration_s: number;
  allowed_window: AllowedWindow | null;
};

// fields reads the declaration form. Only what a form can get wrong on its own
// is checked here; every rule that matters — a URL Ballast may reach, the
// ceilings, a real timezone — is the API's, and its 400 says which.
function fields(form: FormData): Fields | string {
  const text = (name: string) => String(form.get(name) ?? "").trim();
  const whole = (name: string) => Number.parseInt(text(name), 10);

  const result: Fields = {
    name: text("name"),
    base_url: text("base_url"),
    max_vus: whole("max_vus"),
    max_duration_s: whole("max_duration_s"),
    allowed_window: null,
  };
  if (!result.name || !result.base_url) {
    return "An environment needs a name and a base URL.";
  }
  if (!Number.isFinite(result.max_vus) || !Number.isFinite(result.max_duration_s)) {
    return "The ceilings must be whole numbers.";
  }

  if (form.get("window") === "on") {
    const window = { start: text("window_start"), end: text("window_end"), timezone: text("window_timezone") };
    if (!window.start || !window.end || !window.timezone) {
      return "An allowed window needs a start, an end and a timezone.";
    }
    result.allowed_window = window;
  }
  return result;
}

// refusal turns what every environment request can be refused for into a
// sentence, or rethrows what it does not know.
function refusal(error: unknown, next: string, forbidden: string): string {
  if (!(error instanceof ApiError)) throw error;
  switch (error.status) {
    case 401:
      toLogin(next);
    case 403:
      return forbidden;
    case 404:
      return "This environment no longer exists. Reload the page.";
    case 400:
    case 409:
    case 422:
      // The API's messages for these say exactly why, are written for whoever
      // sent the request, and never carry a secret's value or the token.
      return error.message;
  }
  throw error;
}

const cannotManage = "Managing environments takes the maintainer role on this project.";

// createEnvironment declares a target. It arrives unverified, whatever is
// sent: the browser is taken to it, where the verification starts.
export async function createEnvironment(projectId: string, _previous: FormState, form: FormData): Promise<FormState> {
  const body = fields(form);
  if (typeof body === "string") return { error: body };

  const { allowed_window, ...rest } = body;
  let id: string;
  try {
    ({ id } = await apiPost<Environment>(path(projectId), allowed_window ? body : rest));
  } catch (error) {
    return { error: refusal(error, `${page(projectId)}/new`, cannotManage) };
  }

  // The shell around the project names what exists, and it is drawn once per
  // visit to the project: it has to hear about this before the browser lands.
  revalidatePath(`/projects/${projectId}`, "layout");
  redirect(page(projectId, id));
}

// updateEnvironment saves the whole form. The API resets the verification
// when the origin changes, since the proof was served by the old one.
export async function updateEnvironment(
  projectId: string,
  environmentId: string,
  _previous: FormState,
  form: FormData,
): Promise<FormState> {
  const body = fields(form);
  if (typeof body === "string") return { error: body };

  try {
    // allowed_window is always sent: null is how the API is told there is no
    // window any more, and leaving it out would keep the old one.
    await apiPatch<Environment>(path(projectId, environmentId), body);
  } catch (error) {
    return { error: refusal(error, `${page(projectId, environmentId)}/edit`, cannotManage) };
  }

  // The shell around the project names what exists, and it is drawn once per
  // visit to the project: it has to hear about this before the browser lands.
  revalidatePath(`/projects/${projectId}`, "layout");
  redirect(page(projectId, environmentId));
}

export async function deleteEnvironment(projectId: string, environmentId: string): Promise<FormState> {
  try {
    await apiDelete(path(projectId, environmentId));
  } catch (error) {
    return { error: refusal(error, page(projectId, environmentId), cannotManage) };
  }

  // The shell around the project names what exists, and it is drawn once per
  // visit to the project: it has to hear about this before the browser lands.
  revalidatePath(`/projects/${projectId}`, "layout");
  redirect(page(projectId));
}

// verifyEnvironment asks Ballast to fetch the challenge from the target. A
// failure is an answer, not an error: its message says what to fix, and the
// environment is now recorded as failed, so the page is rendered again.
export async function verifyEnvironment(projectId: string, environmentId: string): Promise<FormState> {
  try {
    await apiPost<Environment>(`${path(projectId, environmentId)}/verify`, undefined);
  } catch (error) {
    const message = refusal(error, page(projectId, environmentId), cannotManage);
    refresh();
    return { error: message };
  }

  refresh();
  return {};
}

// approveEnvironment is the administrator's decision that load may reach the
// target. The API checks approve_target and that the proof is in.
export async function approveEnvironment(projectId: string, environmentId: string): Promise<FormState> {
  try {
    await apiPost<Environment>(`${path(projectId, environmentId)}/approve`, undefined);
  } catch (error) {
    const message = refusal(
      error,
      page(projectId, environmentId),
      "Approving a target takes the admin role on this project.",
    );
    refresh();
    return { error: message };
  }

  refresh();
  return {};
}

export type SecretState = { error?: string; saved?: string };

// setSecret stores a value under a key, replacing any value already there. The
// value goes to the API and is never read back: nothing returns it.
export async function setSecret(projectId: string, environmentId: string, _previous: SecretState, form: FormData): Promise<SecretState> {
  const key = String(form.get("key") ?? "").trim();
  const value = String(form.get("value") ?? "");
  if (!key || !value) {
    return { error: "A secret needs a key and a value." };
  }

  try {
    await apiPut(`${path(projectId, environmentId)}/secrets/${encodeURIComponent(key)}`, { value });
  } catch (error) {
    return { error: refusal(error, page(projectId, environmentId), cannotManage) };
  }

  refresh();
  return { saved: key };
}

export async function deleteSecret(projectId: string, environmentId: string, key: string): Promise<FormState> {
  try {
    await apiDelete(`${path(projectId, environmentId)}/secrets/${encodeURIComponent(key)}`);
  } catch (error) {
    return { error: refusal(error, page(projectId, environmentId), cannotManage) };
  }

  refresh();
  return {};
}
