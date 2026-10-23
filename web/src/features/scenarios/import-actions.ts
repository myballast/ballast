"use server";

import { revalidatePath } from "next/cache";
import { redirect } from "next/navigation";

import { ApiError, apiPost } from "@/lib/api-client";

import { filesOf, readRequestsForm } from "./requests-body";
import type {
  CollectionPreview,
  DetectedSecret,
  ImportFinding,
  OpenAPIPreview,
  RequestsFormat,
  Scenario,
} from "./types";

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

export type CollectionPreviewResult = { preview: CollectionPreview } | { error: string };

export type CollectionImportResult =
  | { scenarioId: string; secrets: DetectedSecret[]; findings: ImportFinding[] }
  | { error: string; nameTaken?: boolean };

function formatPath(projectId: string, format: RequestsFormat): string {
  return `/projects/${encodeURIComponent(projectId)}/scenarios/import/${format}`;
}

const unreadable = { error: "The request could not be read. Reload the page and try again." };

// previewRequests sends a file of requests, and an environment file for Postman,
// and gets back the requests it describes. Nothing is stored. The files are text
// read in the browser: no value in them is looked at here. It takes a FormData
// (see requestsForm) because a server action refuses a long string argument.
export async function previewRequests(form: FormData): Promise<CollectionPreviewResult> {
  const request = readRequestsForm(form);
  if (!request) return unreadable;

  try {
    const preview = await apiPost<CollectionPreview>(
      `${formatPath(request.projectId, request.format)}/preview`,
      filesOf(request.format, request.text, request.environment),
    );
    return { preview };
  } catch (error) {
    return refusalOf(error, `/projects/${request.projectId}/scenarios/import`);
  }
}

// importRequests builds the scenario from the chosen requests. It does not take
// the browser anywhere: what comes back is the report and the secrets to create,
// which the person reads before leaving.
export async function importRequests(form: FormData): Promise<CollectionImportResult> {
  const request = readRequestsForm(form);
  if (!request) return unreadable;

  try {
    const result = await apiPost<{ scenario: Scenario; secrets: DetectedSecret[]; findings: ImportFinding[] }>(
      formatPath(request.projectId, request.format),
      {
        name: request.name,
        requests: request.requests,
        ...filesOf(request.format, request.text, request.environment),
      },
    );
    revalidatePath(`/projects/${request.projectId}`, "layout");
    return { scenarioId: result.scenario.id, secrets: result.secrets, findings: result.findings };
  } catch (error) {
    return refusalOf(error, `/projects/${request.projectId}/scenarios/import`);
  }
}
