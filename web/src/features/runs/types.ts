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

export type Run = {
  id: string;
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
};
