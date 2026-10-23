import { can } from "@/lib/permissions";
import type { ScenarioPage } from "@/features/scenarios/detail";

import { Builder } from "./builder";

// BuilderPage turns what the scenario page loads into what the builder takes: the
// verified environments only — nothing else takes load — and what the caller may do.
export function BuilderPage({ page }: { page: ScenarioPage }) {
  const { project, access, scenario, environments } = page;
  if (!scenario.graph) return null;

  return (
    <Builder
      projectId={project.id}
      projectName={project.name}
      scenario={{ ...scenario, graph: scenario.graph }}
      targets={environments.filter((e) => e.verification_status === "verified").map((e) => ({ id: e.id, name: e.name }))}
      canEdit={can(access, "manage_scenarios")}
      canRun={can(access, "run_test")}
      now={new Date()}
    />
  );
}
