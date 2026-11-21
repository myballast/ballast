import { apiGet } from "@/lib/api-client";

import type { ApiTokenList } from "./types";

// getApiTokens are the tokens of a project, revoked ones included. Only an
// administrator may read them; the page asks only when the caller is one.
export async function getApiTokens(projectId: string): Promise<ApiTokenList> {
  return apiGet<ApiTokenList>(`/projects/${encodeURIComponent(projectId)}/api-tokens`);
}
