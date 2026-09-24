import { ApiError, apiGet } from "@/lib/api-client";

import type { Run } from "./types";

// getRun returns null when there is no such run for the caller's organization.
//
// A malformed id is answered the same way as an unknown one: from the outside,
// both mean there is nothing to show at this address.
export async function getRun(id: string): Promise<Run | null> {
  try {
    return await apiGet<Run>(`/runs/${encodeURIComponent(id)}`);
  } catch (error) {
    if (
      error instanceof ApiError &&
      (error.status === 404 || error.status === 400)
    ) {
      return null;
    }
    throw error;
  }
}
