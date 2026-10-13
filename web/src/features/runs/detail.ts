import { getEnvironments } from "@/features/environments/api";
import type { Environment } from "@/features/environments/types";
import { getAccess, getProject } from "@/features/projects/api";
import type { Project } from "@/features/projects/types";
import { getScenario, getScenarioVersion } from "@/features/scenarios/api";
import type { ScenarioSummary, ScenarioVersion } from "@/features/scenarios/types";
import type { Access } from "@/lib/permissions";

import type { Run } from "./types";

// RunContext is everything around a run that the detail page names.
//
// What describes the execution comes from the run itself — profile,
// parameters, verdicts — or from the scenario version it names, which never
// changes. Only the scenario's current name and version are read from the
// scenario as it is now, and they are shown as such: editing the scenario
// after the run cannot change what the page says was executed.
export type RunContext = {
  project: Project;
  access: Access;
  // null when the scenario has since been deleted.
  scenario: ScenarioSummary | null;
  // The version the run executed; null if it cannot be read.
  executed: ScenarioVersion | null;
  environment: Environment | null;
};

export async function loadRunContext(run: Run): Promise<RunContext> {
  const [project, access, scenario, executed, environments] = await Promise.all([
    getProject(run.project_id),
    getAccess(run.project_id),
    getScenario(run.project_id, run.scenario_id),
    getScenarioVersion(run.project_id, run.scenario_id, run.scenario_version),
    getEnvironments(run.project_id),
  ]);

  return {
    project,
    access,
    scenario,
    executed,
    environment: environments.find((e) => e.id === run.environment_id) ?? null,
  };
}
