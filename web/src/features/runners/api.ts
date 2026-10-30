import { ApiError, apiGet } from "@/lib/api-client";

import type { RunnerList } from "./types";

// getRunners reads the machines that execute runs, or nothing for somebody who
// may not.
//
// The API reserves this to an administrator of every project of the organization:
// it is a state of the whole platform, and a picture that leaves out part of it
// would be taken for all of it. Anybody else gets a 403, and for them the card
// simply is not there — not an error page on a screen they were allowed to open.
// Any other failure is not swallowed.
export async function getRunners(): Promise<RunnerList | null> {
  try {
    return await apiGet<RunnerList>("/runners");
  } catch (error) {
    if (error instanceof ApiError && error.status === 403) {
      return null;
    }
    throw error;
  }
}
