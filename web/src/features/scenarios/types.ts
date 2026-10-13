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

// One version of a scenario's script, as it was when it was written. A run
// names the version it executed, and that version never changes.
export type ScenarioVersion = {
  version: number;
  script: string;
  default_profile: ProfileKind;
  default_params: ProfileParams;
  created_at: string;
};
