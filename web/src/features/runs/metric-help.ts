import type { Threshold } from "./types.ts";

// What each number of a run means, in words for somebody who has never done a load
// test. The technical name stays on the card, so that it is learnt, not hidden.
export const metricHelp: Record<string, string> = {
  "p95 latency":
    "95 % of the requests were faster than this. It shows what most users felt, without a few slow outliers.",
  "p99 latency": "Only the slowest 1 % of the requests took longer than this.",
  "Requests / sec": "How many requests the target served each second. The higher, the more load it handled.",
  "Error rate": "The share of requests that failed: a network error, or an error status such as 500.",
  "Median latency": "The typical response time: half of the requests were faster, half slower.",
  Iterations: "How many times the scenario was played from its first step to its last.",
  "Checks passed": "The share of the scenario's own checks that held. A dash means the scenario defines none.",
  "Data received": "The total size of what the target sent back during the run.",
};

export const loadHelp =
  "VUs are virtual users: simulated people running the scenario at the same time. The three durations are the climb to that number (ramp-up), the time spent at full load (plateau), and the wind-down (ramp-down).";

// errorRateNotice is the line under the error rate when a high rate would go
// unnoticed: a run is judged by its thresholds, and with none on failed requests
// it can read "Passed" while every request failed. Null when there is nothing to say.
export function errorRateNotice(errorRate: number | null, thresholds: Threshold[]): string | null {
  if (errorRate === null || !(errorRate > 0)) return null;
  if (thresholds.some((t) => t.metric === "http_req_failed" || t.metric.startsWith("http_req_failed{"))) return null;

  return "No threshold watches the error rate, so failed requests do not fail this run, whatever its status says.";
}
