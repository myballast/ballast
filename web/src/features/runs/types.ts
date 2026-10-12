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
  profile: string;
  params: ProfileParams;
  status: RunStatus;
  error_message?: string;
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
