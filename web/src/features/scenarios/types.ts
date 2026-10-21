import type { ProfileKind, ProfileParams } from "@/features/runs/types";

// Mirror of dto.ScenarioSummary in internal/adapter/http/dto.
export type ScenarioSummary = {
  id: string;
  name: string;
  version: number;
  default_profile: ProfileKind;
  default_params: ProfileParams;
  created_at: string;
  updated_at: string;
};

// Mirror of dto.ScenarioListItem: a scenario as the list shows it.
export type ScenarioListItem = ScenarioSummary & { threshold_count: number };

// Mirror of dto.ScriptFinding. Key on the code; the message is the API's.
export type ScriptFinding = {
  code: string;
  message: string;
  detail?: string;
  // From one; absent when the finding is about the script as a whole.
  line?: number;
};

// Mirror of dto.Scenario: one scenario with its script, and the warnings
// inspecting it raised. Errors never get this far: they refuse the script.
export type Scenario = ScenarioSummary & {
  script: string;
  // "graph" means Ballast generated the script from a graph, and it is read
  // only: the API refuses a new script for it.
  mode: "code" | "graph";
  warnings: ScriptFinding[];
};

// Mirror of dto.ThresholdRule.
export type Threshold = {
  id: string;
  // Accepts k6's tagged syntax: http_req_duration{name:POST /login}.
  metric: string;
  expression: string;
};

// One version of a scenario's script, as it was when it was written. A run
// names the version it executed, and that version never changes.
export type ScenarioVersion = {
  version: number;
  script: string;
  default_profile: ProfileKind;
  default_params: ProfileParams;
  created_at: string;
};

// Mirror of dto.OpenAPIEndpoint: one operation of an uploaded specification.
export type OpenAPIEndpoint = {
  method: string;
  // The template as the specification wrote it: /users/{id}.
  path: string;
  tag: string;
  summary: string;
  path_params: string[];
  has_body: boolean;
  body_too_long: boolean;
};

// Mirror of dto.OpenAPIAuthScheme. It names a scheme and says where it goes;
// it never holds a value.
export type OpenAPIScheme = {
  name: string;
  type: string;
  detail: string;
};

// Mirror of dto.OpenAPIPreviewResponse.
export type OpenAPIPreview = {
  title: string;
  base_path: string;
  endpoints: OpenAPIEndpoint[];
  schemes: OpenAPIScheme[];
  notes: string[];
};
