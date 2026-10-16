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
