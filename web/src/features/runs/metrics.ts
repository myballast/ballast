import type { Metric } from "./types";

// The four numbers a run is judged on at a glance.
export type HeadlineMetrics = {
  p95Ms: number | null;
  p99Ms: number | null;
  requestsPerSecond: number | null;
  errorRate: number | null;
};

// headlineMetrics picks k6's built-in metrics out of a run's results.
//
// Only the untagged series is read: http_req_duration{expected_response:true}
// is a sub-metric k6 adds on its own, and it leaves the failed requests out.
// A number k6 did not report comes back as null, never as zero — a run that
// errored before sending anything has no latency, not a latency of 0 ms.
export function headlineMetrics(metrics: Metric[]): HeadlineMetrics {
  return {
    p95Ms: pick(metrics, "http_req_duration", "p(95)"),
    p99Ms: pick(metrics, "http_req_duration", "p(99)"),
    requestsPerSecond: pick(metrics, "http_reqs", "rate"),
    errorRate: pick(metrics, "http_req_failed", "rate"),
  };
}

function pick(metrics: Metric[], metric: string, stat: string): number | null {
  const found = metrics.find(
    (m) => m.metric === metric && !m.tag && m.stat === stat,
  );
  return found?.value ?? null;
}
