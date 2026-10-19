"use client";

import { Play, TriangleAlert } from "lucide-react";
import Link from "next/link";
import { useCallback, useId, useState, useTransition } from "react";

import { Time } from "@/components/time";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { launchDefaultRun } from "@/features/runs/actions";

import type { Scenario } from "../types";
import { profileSummary, runBlockedReason } from "../workspace";
import { ScenarioForm } from "./scenario-form";
import { ScriptEditor } from "./script-editor";
import { useUnsavedGuard } from "../use-unsaved-guard";

type Target = { id: string; name: string };

// ScenarioWorkspace is the top of a scenario's page and what is under it: the bar
// that says which scenario this is, what it runs and against what — and runs it —
// and the form that edits it. They are one component because they share one fact:
// whether what is on screen is what is saved. A run always executes a saved
// version, so the bar holds Run back while the form differs, and says why.
//
// children are the parts of the page that need none of that — the script's
// warnings — and sit between the two.
export function ScenarioWorkspace({
  projectId,
  projectName,
  scenario,
  targets,
  canEdit,
  canRun,
  now,
  children,
}: {
  projectId: string;
  projectName: string;
  scenario: Scenario;
  // The verified environments, and only those: nothing else takes load.
  targets: Target[];
  canEdit: boolean;
  canRun: boolean;
  now: Date;
  children?: React.ReactNode;
}) {
  const [dirty, setDirty] = useState(false);
  const onDirtyChange = useCallback((next: boolean) => setDirty(next), []);
  const guard = useUnsavedGuard(dirty);

  const [environmentId, setEnvironmentId] = useState(targets[0]?.id ?? "");
  const [error, setError] = useState<string | null>(null);
  const [pending, startTransition] = useTransition();
  const reason = useId();

  const blocked = runBlockedReason({ dirty, canRun, verifiedEnvironments: targets.length });
  const own = scenario.default_params.target_vus >= 1;

  function run() {
    setError(null);
    startTransition(async () => {
      const outcome = await launchDefaultRun(projectId, scenario.id, environmentId);
      if (outcome?.error) setError(outcome.error);
    });
  }

  return (
    <div className="flex flex-col gap-6">
      {guard}

      <div className="sticky top-0 z-10 -mx-4 flex flex-col gap-2 border-b bg-background/95 px-4 py-3 backdrop-blur md:-mx-page-x md:px-page-x">
        <Link href={`/projects/${projectId}/scenarios`} className="w-fit text-xs text-muted-foreground hover:underline">
          {projectName} · Scenarios
        </Link>

        <div className="flex flex-wrap items-center gap-x-3 gap-y-2">
          <h1 className="text-xl font-medium tracking-tight">{scenario.name}</h1>
          <span className="data text-xs text-muted-foreground">v{scenario.version}</span>

          <span
            role="status"
            className="data flex items-center gap-1.5 rounded-md border px-2 py-0.5 text-[11px] text-muted-foreground"
          >
            {dirty ? (
              <>
                <TriangleAlert aria-hidden className="size-3" />
                unsaved changes
              </>
            ) : (
              <>
                saved <Time iso={scenario.updated_at} mode="relative" now={now} />
              </>
            )}
          </span>

          <span className="h-5 w-px bg-border" aria-hidden />

          <span className="data text-xs text-muted-foreground" title="The default profile a run starts from">
            {own ? profileSummary(scenario.default_profile, scenario.default_params) : `${scenario.default_profile} template`}
          </span>

          {canRun && (
            <div className="ml-auto flex flex-wrap items-center gap-2">
              <label className="flex items-center gap-2 text-xs text-muted-foreground">
                target
                <select
                  value={environmentId}
                  onChange={(event) => setEnvironmentId(event.target.value)}
                  disabled={targets.length === 0}
                  aria-describedby={blocked ? reason : undefined}
                  className="data h-8 rounded-md border bg-background px-2 text-xs text-foreground outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:opacity-50"
                >
                  {targets.length === 0 && <option value="">no verified environment</option>}
                  {targets.map((target) => (
                    <option key={target.id} value={target.id}>
                      {target.name}
                    </option>
                  ))}
                </select>
              </label>

              <Button
                onClick={run}
                disabled={blocked !== null || pending || !environmentId}
                aria-describedby={blocked ? reason : undefined}
              >
                <Play aria-hidden />
                {pending ? "Starting…" : "Run"}
              </Button>
            </div>
          )}
        </div>

        {/* Why Run is held back, in words and next to the button: a disabled button
            that says nothing is a question with no answer. */}
        {canRun && blocked && (
          <p id={reason} className="text-xs text-muted-foreground">
            {blocked}
            {targets.length === 0 && !dirty && (
              <>
                {" "}
                <Link href={`/projects/${projectId}/environments`} className="text-brand hover:underline">
                  Go to the environments
                </Link>
              </>
            )}
          </p>
        )}
        {error && (
          <p role="alert" className="text-xs text-muted-foreground">
            {error}
          </p>
        )}
      </div>

      {children}

      <Card>
        <CardHeader>
          <CardTitle className="text-[11px] font-normal tracking-wider text-muted-foreground uppercase">
            {canEdit ? "Scenario" : "Script"}
          </CardTitle>
        </CardHeader>
        <CardContent>
          {canEdit ? (
            // Keyed on the version, so that a saved change starts the form again
            // from what was stored.
            <ScenarioForm
              key={scenario.version}
              projectId={projectId}
              scenario={scenario}
              onDirtyChange={onDirtyChange}
            />
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
    </div>
  );
}
