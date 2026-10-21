"use server";

import { revalidatePath } from "next/cache";
import { redirect } from "next/navigation";

import { ApiError, apiPost } from "@/lib/api-client";

import type { OpenAPIPreview, Scenario } from "./types";

function path(projectId: string): string {
  return `/projects/${encodeURIComponent(projectId)}/scenarios/import/openapi`;
}

function toLogin(next: string): never {
  redirect(`/login?next=${encodeURIComponent(next)}`);
}

// What the import screen is told back. A refusal carries a sentence the API
// wrote about the file or the choice: those never repeat anything taken from the
// file, so they are shown as they are.
export type PreviewResult = { preview: OpenAPIPreview } | { error: string };
export type ImportResult = { error: string; nameTaken?: boolean };

function refusalOf(error: unknown, next: string): { error: string; nameTaken?: boolean } {
  if (!(error instanceof ApiError)) throw error;

  switch (error.status) {
    case 401:
      toLogin(next);
    case 403:
      return { error: "Writing scenarios takes the maintainer role on this project." };
    case 409:
      return { error: "Another scenario of this project already uses that name.", nameTaken: true };
    case 413:
      return { error: error.message };
    case 400:
    case 422:
      return { error: error.message };
  }
  throw error;
}

// previewOpenAPI sends the text of a specification and gets back the endpoints it
// describes. Nothing is stored.
export async function previewOpenAPI(projectId: string, spec: string): Promise<PreviewResult> {
  try {
    const preview = await apiPost<OpenAPIPreview>(`${path(projectId)}/preview`, { spec });
    return { preview };
  } catch (error) {
    return refusalOf(error, `/projects/${projectId}/scenarios/import`);
  }
}

// importOpenAPI builds the scenario from the chosen endpoints and takes the
// browser to it. It only returns when the import was refused.
export async function importOpenAPI(
  projectId: string,
  name: string,
  spec: string,
  endpoints: { method: string; path: string }[],
): Promise<ImportResult> {
  let id: string;
  try {
    ({
      scenario: { id },
    } = await apiPost<{ scenario: Scenario }>(path(projectId), { name, spec, endpoints }));
  } catch (error) {
    return refusalOf(error, `/projects/${projectId}/scenarios/import`);
  }

  // The shell around the project names what exists, and it is drawn once per
  // visit to the project: it has to hear about this before the browser lands.
  revalidatePath(`/projects/${projectId}`, "layout");
  redirect(`/projects/${projectId}/scenarios/${id}`);
}
