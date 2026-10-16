import { cache } from "react";

import { apiGet } from "@/lib/api-client";
import { formatCount, formatMs, formatPercent, formatSeconds, missing } from "@/lib/format";

// Mirror of dto.ProjectStatsResponse in internal/adapter/http/dto. A null is
// "nothing to say" and never zero.
export type StatsMeasure = {
  value: number | null;
  previous: number | null;
  change: number | null;
  // A point for each day of the window, the oldest first.
  series: (number | null)[];
};

export type ProjectStats = {
  days: number;
  from: string;
  to: string;
  pass_rate: StatsMeasure;
  failed_runs: StatsMeasure;
  total_runs: StatsMeasure;
  avg_duration_s: StatsMeasure;
  p95_ms: StatsMeasure;
};

// The figures are added up in SQL by the API. Nothing here computes one: this
// reads them, and puts them into words.
export const getProjectStats = cache(async (projectId: string, days = 30): Promise<ProjectStats> => {
  return apiGet<ProjectStats>(`/projects/${encodeURIComponent(projectId)}/stats?days=${days}`);
});

export type Tile = {
  label: string;
  value: string;
  // How the figure moved from the window before, in words; null when there is
  // nothing to compare it with.
  change: string | null;
  direction: "up" | "down" | "flat" | null;
  series: (number | null)[];
};

const oneDecimal = new Intl.NumberFormat("en-GB", { minimumFractionDigits: 1, maximumFractionDigits: 1 });

// A change of a percentage is read to the unit once it is large enough to be
// round, and to a tenth below that.
function percent(change: number): string {
  const size = Math.abs(change);
  return `${size < 10 ? oneDecimal.format(size) : Math.round(size)}%`;
}

function direction(change: number | null): Tile["direction"] {
  if (change === null) return null;
  return change > 0 ? "up" : change < 0 ? "down" : "flat";
}

// tilesOf is the five tiles of the project's page. A window with no run has
// nothing in it — not zeros: a pass rate of 0% and a project nobody has tested
// are not the same thing — so every tile says so with a dash.
export function tilesOf(stats: ProjectStats): Tile[] {
  const empty = stats.total_runs.value === 0;

  const tile = (
    label: string,
    measure: StatsMeasure,
    value: (v: number) => string,
    change: (c: number) => string,
  ): Tile => {
    if (empty || measure.value === null) {
      return { label, value: missing, change: null, direction: null, series: [] };
    }
    const moved = measure.change;
    return {
      label,
      value: value(measure.value),
      change: moved === null ? null : moved === 0 ? "no change" : change(Math.abs(moved)),
      direction: direction(moved),
      series: measure.series,
    };
  };

  return [
    tile("Pass rate", stats.pass_rate, formatPercent, (c) => `${oneDecimal.format(c)} pts`),
    tile("Failed runs", stats.failed_runs, formatCount, formatCount),
    tile("Total runs", stats.total_runs, formatCount, percent),
    tile("Avg duration", stats.avg_duration_s, formatSeconds, formatSeconds),
    tile("p95 latency", stats.p95_ms, formatMs, formatMs),
  ];
}
