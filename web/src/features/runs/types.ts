// Mirrors of the shapes in internal/adapter/http/dto/run.go. The JSON field
// names are a promise made there; keep both sides in step.

export type RunStatus =
  | "queued"
  | "running"
  | "passed"
  | "failed"
  | "errored"
  | "timeout"
  | "cancelled"
  | "rejected"
  | "skipped"
  | "orphaned";

// Every status, in the order the lifecycle reads: the options of a filter.
export const runStatuses: RunStatus[] = [
  "queued",
  "running",
  "passed",
  "failed",
  "errored",
  "timeout",
  "cancelled",
  "rejected",
  "skipped",
  "orphaned",
];

export type ProfileKind = "smoke" | "load" | "stress" | "spike" | "soak";

export const profileKinds: ProfileKind[] = ["smoke", "load", "stress", "spike", "soak"];

// A run in one of these may still be stopped.
export const stoppable: RunStatus[] = ["queued", "running"];

export type ProfileParams = {
  target_vus: number;
  ramp_up_s: number;
  plateau_s: number;
  ramp_down_s: number;
};

export type Metric = {
  metric: string;
  tag?: string;
  stat: string;
  value: number;
};

export type Threshold = {
  metric: string;
  expression: string;
  passed: boolean;
};

// RunCI is what a pipeline said about itself when it launched a run: which CI,
// which repository, branch and commit, and the link to the job. Only the provider
// is always there. The API keeps it as it was declared, bounded and tidied; the
// screen draws it as text and follows the link only if it is an https address.
export type RunCI = {
  provider: string;
  repository?: string;
  branch?: string;
  commit?: string;
  job_url?: string;
};

export type Run = {
  id: string;
  // How the run is spoken of within its project: #1482. Never reused.
  number: number;
  // Who started it, by name. Absent when nobody did, or the person has since
  // been removed.
  triggered_by?: string;
  // What the pipeline that launched the run said about itself. Absent for a run
  // nobody said anything about. Declared by the caller and proved by nobody.
  ci?: RunCI;
  project_id: string;
  scenario_id: string;
  environment_id: string;
  scenario_version: number;
  profile: ProfileKind;
  params: ProfileParams;
  status: RunStatus;
  error_message?: string;
  // Set as soon as somebody asks a running run to stop, before it has.
  cancel_requested_at?: string;
  created_at: string;
  started_at?: string;
  ended_at?: string;
  metrics: Metric[];
  thresholds: Threshold[];
};

// LogLine is one line of a run's output, as GET /runs/{id}/logs streams it.
//
// seq is also the event's id, which is what the browser sends back as
// Last-Event-ID when it reconnects.
export type LogLine = {
  seq: number;
  stream: "stdout" | "stderr";
  message: string;
  at: string;
};

// A run as GET /projects/{projectId}/runs lists it: no metrics, no verdicts.
export type RunSummary = {
  id: string;
  number: number;
  project_id: string;
  scenario_id: string;
  environment_id: string;
  scenario_version: number;
  profile: ProfileKind;
  params: ProfileParams;
  status: RunStatus;
  created_at: string;
  started_at?: string;
  ended_at?: string;
  // Who started it, by name. Absent when nobody did.
  triggered_by?: string;
  // The headline numbers of a finished run, as k6 reported them: the 95th
  // percentile of the request duration in ms, and the share of failed
  // requests as a ratio. Absent while the run has no results.
  p95_ms?: number;
  error_rate?: number;
};
