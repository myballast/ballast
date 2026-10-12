import { apiGet } from "@/lib/api-client";

import type { Environment } from "./types";

export async function getEnvironments(projectId: string): Promise<Environment[]> {
  const { environments } = await apiGet<{ environments: Environment[] }>(
    `/projects/${encodeURIComponent(projectId)}/environments`,
  );
  return environments;
}
