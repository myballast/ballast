import type { Metric } from "./types";

// The numbers a run is judged on at a glance.
export type HeadlineMetrics = {
  p95Ms: number | null;
  p99Ms: number | null;
  requestsPerSecond: number | null;
  errorRate: number | null;
  medianMs: number | null;
  iterations: number | null;
  checksRate: number | null;
  bytesReceived: number | null;
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
    medianMs: pick(metrics, "http_req_duration", "med"),
    iterations: pick(metrics, "iterations", "count"),
    checksRate: pick(metrics, "checks", "rate"),
    bytesReceived: pick(metrics, "data_received", "count"),
  };
}

// The six phases of an HTTP request, in the order they happen. k6 measures
// each one on its own; their sum is http_req_duration's companion, not a
// replacement for it (blocked and connecting are outside it).
export const phases = [
  { metric: "http_req_blocked", label: "Blocked", hint: "waiting for a free connection" },
  { metric: "http_req_connecting", label: "Connecting", hint: "TCP connection" },
  { metric: "http_req_tls_handshaking", label: "TLS", hint: "TLS handshake" },
  { metric: "http_req_sending", label: "Sending", hint: "sending the request" },
  { metric: "http_req_waiting", label: "Waiting", hint: "time to first byte" },
  { metric: "http_req_receiving", label: "Receiving", hint: "reading the response" },
] as const;

export type TrendStats = {
  avg: number | null;
  med: number | null;
  p95: number | null;
  p99: number | null;
  max: number | null;
};

export function trend(metrics: Metric[], metric: string, tag?: string): TrendStats {
  const at = (stat: string) => pick(metrics, metric, stat, tag);
  return { avg: at("avg"), med: at("med"), p95: at("p(95)"), p99: at("p(99)"), max: at("max") };
}

// EndpointMetrics is one tagged sub-metric selector — "name:POST /login" —
// with what k6 reported under it.
export type EndpointMetrics = {
  tag: string;
  duration: TrendStats | null;
  failedRate: number | null;
  requests: number | null;
};

// k6 adds this sub-metric on its own, to every run: it is the successful
// responses, not an endpoint.
const builtInTags = new Set(["expected_response:true"]);

// endpointMetrics lists the sub-metrics k6 reported, one per tag selector.
// k6 computes a sub-metric only for a selector that a threshold names, so
// these are the endpoints the scenario declared thresholds on, and only them.
export function endpointMetrics(metrics: Metric[]): EndpointMetrics[] {
  const tags = [...new Set(metrics.map((m) => m.tag).filter((t): t is string => !!t && !builtInTags.has(t)))];
  tags.sort();

  return tags.map((tag) => {
    const has = (metric: string) => metrics.some((m) => m.metric === metric && m.tag === tag);
    return {
      tag,
      duration: has("http_req_duration") ? trend(metrics, "http_req_duration", tag) : null,
      failedRate: pick(metrics, "http_req_failed", "rate", tag),
      requests: pick(metrics, "http_reqs", "count", tag),
    };
  });
}

// pick reads one statistic. Without tag, the untagged series only.
function pick(metrics: Metric[], metric: string, stat: string, tag?: string): number | null {
  const found = metrics.find(
    (m) => m.metric === metric && (tag ? m.tag === tag : !m.tag) && m.stat === stat,
  );
  return found?.value ?? null;
}

// Results is where a run's results stand: still to come, never reported (the
// run ended before k6 could write its summary), or in.
export type Results = "pending" | "missing" | "reported";

export function resultsOf(run: { ended_at?: string; metrics: Metric[] }): Results {
  if (!run.ended_at) return "pending";
  return run.metrics.length > 0 ? "reported" : "missing";
}

// What a results section says when it has nothing to show, and why.
export function noResults(results: Results, what: string): string | null {
  switch (results) {
    case "pending":
      return `${what} appear when the run ends.`;
    case "missing":
      return "The run ended before k6 reported any result.";
    default:
      return null;
  }
}
