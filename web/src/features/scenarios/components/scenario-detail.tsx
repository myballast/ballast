import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { can } from "@/lib/permissions";

import type { ScenarioPage } from "../detail";
import { ScenarioWorkspace } from "./scenario-workspace";
import { ScriptFindings } from "./script-findings";
import { ThresholdRules } from "./threshold-rules";

function Title({ children }: { children: React.ReactNode }) {
  return (
    <CardTitle className="text-[11px] font-normal tracking-wider text-muted-foreground uppercase">{children}</CardTitle>
  );
}

// ScenarioDetail is one scenario: its bar and form for whoever may write
// scenarios, the same content read-only for everyone else, and its thresholds.
export function ScenarioDetail({ page }: { page: ScenarioPage }) {
  const { project, access, scenario, thresholds, environments } = page;
  const editable = can(access, "manage_scenarios");

  return (
    <div className="flex flex-col gap-8">
      <ScenarioWorkspace
        projectId={project.id}
        projectName={project.name}
        scenario={scenario}
        targets={environments
          .filter((e) => e.verification_status === "verified")
          .map((e) => ({ id: e.id, name: e.name }))}
        canEdit={editable}
        canRun={can(access, "run_test")}
        now={new Date()}
      >
        <p className="text-xs text-muted-foreground">
          Runs already recorded keep the version they executed.
        </p>
        <ScriptFindings findings={scenario.warnings} kind="warning" />
      </ScenarioWorkspace>

      <Card>
        <CardHeader>
          <Title>Thresholds</Title>
        </CardHeader>
        <CardContent>
          <ThresholdRules
            projectId={project.id}
            scenarioId={scenario.id}
            thresholds={thresholds}
            editable={editable}
          />
        </CardContent>
      </Card>
    </div>
  );
}
