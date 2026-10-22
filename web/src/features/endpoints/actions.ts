"use server";

import { revalidatePath } from "next/cache";
import { redirect } from "next/navigation";

import { ApiError, apiDelete, apiPost } from "@/lib/api-client";

function path(projectId: string): string {
  return `/projects/${encodeURIComponent(projectId)}/endpoints`;
}

function toLogin(next: string): never {
  redirect(`/login?next=${encodeURIComponent(next)}`);
}

// What a catalog action tells back: a sentence when it was refused. The API's own
// sentences about a file or a source name never repeat anything from the file,
// so they are shown as they are.
export type CatalogOutcome = { error?: string };

function refusalOf(error: unknown, next: string): { error: string } {
  if (!(error instanceof ApiError)) throw error;

  switch (error.status) {
    case 401:
      toLogin(next);
    case 403:
      return { error: "Changing the endpoints takes the maintainer role on this project." };
    case 404:
      return { error: "This endpoint no longer exists. Reload the page." };
    case 400:
    case 413:
    case 422:
      return { error: error.message };
  }
  throw error;
}

// importToCatalog adds the chosen endpoints of a specification to the project's
// catalog, replacing what the same source had before.
export async function importToCatalog(
  projectId: string,
  source: string,
  spec: string,
  endpoints: { method: string; path: string }[],
): Promise<CatalogOutcome & { count?: number }> {
  try {
    const { count } = await apiPost<{ count: number }>(`${path(projectId)}/import/openapi`, {
      source,
      spec,
      endpoints,
    });
    revalidatePath(`/projects/${projectId}`, "layout");
    return { count };
  } catch (error) {
    return refusalOf(error, `/projects/${projectId}/scenarios/import`);
  }
}

// importCollectionToCatalog adds the chosen requests of a Postman collection to
// the project's catalog, replacing what the same source had before. The files are
// sent again: nothing is kept on the server between the preview and this.
export async function importCollectionToCatalog(
  projectId: string,
  source: string,
  collection: string,
  environment: string,
  requests: number[],
): Promise<CatalogOutcome & { count?: number; findings?: { request: string; message: string }[] }> {
  try {
    const result = await apiPost<{ count: number; findings: { request: string; message: string }[] }>(
      `${path(projectId)}/import/postman`,
      { source, collection, environment, requests },
    );
    revalidatePath(`/projects/${projectId}`, "layout");
    return result;
  } catch (error) {
    return refusalOf(error, `/projects/${projectId}/scenarios/import`);
  }
}

export async function deleteCatalogEndpoint(projectId: string, endpointId: string): Promise<CatalogOutcome> {
  try {
    await apiDelete(`${path(projectId)}/${encodeURIComponent(endpointId)}`);
  } catch (error) {
    return refusalOf(error, `/projects/${projectId}/endpoints`);
  }
  revalidatePath(`/projects/${projectId}/endpoints`);
  return {};
}

export async function deleteCatalogSource(projectId: string, source: string): Promise<CatalogOutcome> {
  try {
    await apiDelete(`${path(projectId)}?source=${encodeURIComponent(source)}`);
  } catch (error) {
    return refusalOf(error, `/projects/${projectId}/endpoints`);
  }
  revalidatePath(`/projects/${projectId}/endpoints`);
  return {};
}
