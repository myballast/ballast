import { cache } from "react";
import { redirect } from "next/navigation";

import { ApiError, apiGet } from "@/lib/api-client";

import type { Run, RunSummary } from "./types";

// getRun returns null when there is no such run for the caller's organization.
//
// A malformed id is answered the same way as an unknown one: from the outside,
// both mean there is nothing to show at this address. Without a session, the
// browser is sent to sign in and brought back here afterwards.
//
// Cached for the request: the shell around the page learns the run's project
// from it, and the page reads the same run.
export const getRun = cache(async (id: string): Promise<Run | null> => {
  try {
    return await apiGet<Run>(`/runs/${encodeURIComponent(id)}`);
  } catch (error) {
    if (error instanceof ApiError && error.status === 401) {
      redirect(`/login?next=${encodeURIComponent(`/runs/${id}`)}`);
    }
    if (
      error instanceof ApiError &&
      (error.status === 404 || error.status === 400)
    ) {
      return null;
    }
    throw error;
  }
});

// next is where the older runs begin and prev where the newer ones do. Both are
// opaque: the API alone knows what is in them.
export type RunPage = { runs: RunSummary[]; next?: string; prev?: string };

export type RunQuery = {
  limit?: number;
  // The page of older runs after this cursor (a page's next), or the page of
  // newer runs before it (a page's prev). At most one.
  before?: string;
  after?: string;
  status?: string;
  scenarioId?: string;
  environmentId?: string;
};

// getProjectRuns is one page of a project's runs, newest first.
export async function getProjectRuns(projectId: string, options: RunQuery = {}): Promise<RunPage> {
  const query = new URLSearchParams();
  if (options.limit) query.set("limit", String(options.limit));
  if (options.before) query.set("before", options.before);
  if (options.after) query.set("after", options.after);
  if (options.status) query.set("status", options.status);
  if (options.scenarioId) query.set("scenario_id", options.scenarioId);
  if (options.environmentId) query.set("environment_id", options.environmentId);
  return apiGet<RunPage>(`/projects/${encodeURIComponent(projectId)}/runs?${query}`);
}

// getLastRun is a project's most recent run, or null if it has never run.
export async function getLastRun(projectId: string): Promise<RunSummary | null> {
  const { runs } = await getProjectRuns(projectId, { limit: 1 });
  return runs[0] ?? null;
}
