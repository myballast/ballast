import { apiGet } from "@/lib/api-client";

import type { CatalogEndpoint } from "./types";

// getCatalog reads every endpoint of a project's catalog, tagged ones first.
export async function getCatalog(projectId: string): Promise<CatalogEndpoint[]> {
  const { endpoints } = await apiGet<{ endpoints: CatalogEndpoint[] }>(
    `/projects/${encodeURIComponent(projectId)}/endpoints`,
  );
  return endpoints;
}

// getCatalogOrNothing is for the screens that only offer the catalog as a help,
// such as the script editor: a catalog that cannot be read must not stop
// somebody from writing a script.
export async function getCatalogOrNothing(projectId: string): Promise<CatalogEndpoint[]> {
  try {
    return await getCatalog(projectId);
  } catch {
    return [];
  }
}
