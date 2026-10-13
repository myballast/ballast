import { formatDateTime } from "@/lib/format";

import { getProjectRuns } from "./api";
import type { RunStatus } from "./types";

// HistoryRow is a run as the history table shows it, already put into words on
// the server. Dates in particular are formatted there, once: a client
// formatting them again in another timezone would not match what the server
// rendered.
export type HistoryRow = {
  id: string;
  status: RunStatus;
  scenario: string;
  environment: string;
  profile: string;
  createdAt: string;
};

export type HistoryPage = { rows: HistoryRow[]; next?: string };

// Names are looked up in what the page already read. A scenario or an
// environment deleted since keeps its run in the history, under its id.
export type Names = { scenarios: Record<string, string>; environments: Record<string, string> };

export const historyPageSize = 20;

export async function historyPage(projectId: string, names: Names, before?: string): Promise<HistoryPage> {
  const page = await getProjectRuns(projectId, { before, limit: historyPageSize });

  return {
    next: page.next,
    rows: page.runs.map((run) => ({
      id: run.id,
      status: run.status,
      scenario: names.scenarios[run.scenario_id] ?? run.scenario_id.slice(0, 8),
      environment: names.environments[run.environment_id] ?? run.environment_id.slice(0, 8),
      profile: run.profile,
      createdAt: formatDateTime(run.created_at),
    })),
  };
}
