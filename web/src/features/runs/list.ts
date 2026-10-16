import { ApiError } from "@/lib/api-client";
import { formatCount, formatDateTime, formatDuration, formatMs, formatPercent, missing } from "@/lib/format";
import { getEnvironments } from "@/features/environments/api";
import { getScenarios } from "@/features/scenarios/api";

import { getProjectRuns } from "./api";
import { runStatuses, type RunStatus } from "./types";

// How many runs a page of the list holds.
export const pageSize = 25;

export type RunFilters = { status?: RunStatus; scenario?: string; environment?: string };

// RunRow is a run as the table shows it, already put into words on the server:
// dates and numbers are formatted once, there, so that a browser in another
// timezone cannot render them differently from the page it was sent.
export type RunRow = {
  id: string;
  number: number;
  status: RunStatus;
  createdAt: string;
  scenario: string;
  environment: string;
  triggeredBy: string;
  p95: string;
  errors: string;
  vus: string;
  duration: string;
};

export type Option = { id: string; name: string };

export type RunsPage = {
  rows: RunRow[];
  filters: RunFilters;
  scenarios: Option[];
  environments: Option[];
  // Opaque cursors, as the API gave them. Next goes to older runs, prev to
  // newer ones.
  next?: string;
  prev?: string;
  // Whether the page is a window into the history rather than its head.
  inHistory: boolean;
};

type SearchParams = Record<string, string | string[] | undefined>;

function one(params: SearchParams, name: string): string | undefined {
  const value = params[name];
  const first = Array.isArray(value) ? value[0] : value;
  return first ? first : undefined;
}

// loadRunsPage reads one page of a project's runs for the address it was asked
// at. Filters come from the query string and are kept only when they name
// something that exists in this project: a stale link to a deleted scenario
// shows the whole list rather than an error.
export async function loadRunsPage(projectId: string, params: SearchParams): Promise<RunsPage> {
  const [scenarios, environments] = await Promise.all([getScenarios(projectId), getEnvironments(projectId)]);

  const status = one(params, "status");
  const scenario = one(params, "scenario");
  const environment = one(params, "environment");
  const filters: RunFilters = {
    status: runStatuses.find((s) => s === status),
    scenario: scenarios.find((s) => s.id === scenario)?.id,
    environment: environments.find((e) => e.id === environment)?.id,
  };

  const before = one(params, "before");
  const after = before ? undefined : one(params, "after");

  const ask = (window: { before?: string; after?: string }) =>
    getProjectRuns(projectId, {
      limit: pageSize,
      status: filters.status,
      scenarioId: filters.scenario,
      environmentId: filters.environment,
      ...window,
    });

  let page;
  let inHistory = Boolean(before || after);
  try {
    page = await ask({ before, after });
  } catch (error) {
    // A cursor the API did not give out — an edited link — is not worth an
    // error page: the head of the list is what was meant.
    if (!(error instanceof ApiError) || error.status !== 400 || !inHistory) throw error;
    page = await ask({});
    inHistory = false;
  }

  const scenarioNames = new Map(scenarios.map((s) => [s.id, s.name]));
  const environmentNames = new Map(environments.map((e) => [e.id, e.name]));

  return {
    filters,
    inHistory,
    next: page.next,
    prev: page.prev,
    scenarios: scenarios.map((s) => ({ id: s.id, name: s.name })),
    environments: environments.map((e) => ({ id: e.id, name: e.name })),
    rows: page.runs.map((run) => ({
      id: run.id,
      number: run.number,
      status: run.status,
      createdAt: formatDateTime(run.created_at),
      // A scenario or an environment deleted since keeps its run in the
      // history; its name is gone, and an identifier would say nothing.
      scenario: scenarioNames.get(run.scenario_id) ?? "Deleted scenario",
      environment: environmentNames.get(run.environment_id) ?? "Deleted environment",
      triggeredBy: run.triggered_by ?? missing,
      p95: formatMs(run.p95_ms ?? null),
      errors: formatPercent(run.error_rate ?? null),
      vus: formatCount(run.params.target_vus),
      duration: formatDuration(run.started_at, run.ended_at),
    })),
  };
}

// runsHref is the address of the list with these filters and, if given, a
// window on the history. The filters travel with the cursors, since a cursor
// is a place in one filtered list and means nothing in another.
export function runsHref(
  projectId: string,
  filters: RunFilters,
  window?: { before?: string; after?: string },
): string {
  const query = new URLSearchParams();
  if (filters.status) query.set("status", filters.status);
  if (filters.scenario) query.set("scenario", filters.scenario);
  if (filters.environment) query.set("environment", filters.environment);
  if (window?.before) query.set("before", window.before);
  if (window?.after) query.set("after", window.after);

  const text = query.toString();
  return `/projects/${projectId}/runs${text ? `?${text}` : ""}`;
}
