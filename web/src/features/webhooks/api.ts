import { ApiError, apiGet } from "@/lib/api-client";

import type { Webhook } from "./types";

// getWebhook is the project's webhook, or null when it has none. Only an administrator
// may read it; the page asks only when the caller is one.
export async function getWebhook(projectId: string): Promise<Webhook | null> {
  try {
    return await apiGet<Webhook>(`/projects/${encodeURIComponent(projectId)}/webhook`);
  } catch (error) {
    if (error instanceof ApiError && error.status === 404) return null;
    throw error;
  }
}
