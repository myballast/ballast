import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { formatDateTime } from "@/lib/format";
import { can } from "@/lib/permissions";
import Link from "next/link";

import type { ScenarioPage } from "../detail";
import { ScenarioForm } from "./scenario-form";
import { ScriptEditor } from "./script-editor";
import { ScriptFindings } from "./script-findings";
import { ThresholdRules } from "./threshold-rules";

function Title({ children }: { children: React.ReactNode }) {
  return (
    <CardTitle className="text-[11px] font-normal tracking-wider text-muted-foreground uppercase">{children}</CardTitle>
  );
}

// ScenarioDetail is one scenario: its form for whoever may write scenarios,
// the same content read-only for everyone else, and its thresholds.
export function ScenarioDetail({ page }: { page: ScenarioPage }) {
  const { project, access, scenario, thresholds } = page;
  const editable = can(access, "manage_scenarios");

  return (
    <div className="flex flex-col gap-8">
      <header className="flex flex-col gap-1">
        <Link href={`/projects/${project.id}/scenarios`} className="text-xs text-muted-foreground hover:underline">
          {project.name} · Scenarios
        </Link>
        <div className="flex flex-wrap items-baseline gap-3">
          <h1 className="text-3xl font-normal tracking-tight">{scenario.name}</h1>
          <span className="data text-sm text-muted-foreground">v{scenario.version}</span>
        </div>
        <p className="text-xs text-muted-foreground">
          updated <span className="num">{formatDateTime(scenario.updated_at)}</span>. Runs already recorded keep the
          version they executed.
        </p>
      </header>

      <ScriptFindings findings={scenario.warnings} kind="warning" />

      <Card>
        <CardHeader>
          <Title>{editable ? "Scenario" : "Script"}</Title>
        </CardHeader>
        <CardContent>
          {editable ? (
            // Keyed on the version, so that a saved change starts the form
            // again from what was stored.
            <ScenarioForm key={scenario.version} projectId={project.id} scenario={scenario} />
          ) : (
            <div className="flex flex-col gap-3 text-sm">
              <ScriptEditor value={scenario.script} readOnly />
              <p className="text-xs text-muted-foreground">
                Default profile <span className="data">{scenario.default_profile}</span> ·{" "}
                <span className="num">{scenario.default_params.target_vus}</span> VUs ·{" "}
                <span className="num">
                  {scenario.default_params.ramp_up_s}s / {scenario.default_params.plateau_s}s /{" "}
                  {scenario.default_params.ramp_down_s}s
                </span>
              </p>
            </div>
          )}
        </CardContent>
      </Card>

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
