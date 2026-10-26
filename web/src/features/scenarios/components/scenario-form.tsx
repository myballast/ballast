"use client";

import { Info, Save } from "lucide-react";
import Link from "next/link";
import { useActionState, useEffect, useState } from "react";

import { Button, buttonVariants } from "@/components/ui/button";
import type { CatalogEndpoint } from "@/features/endpoints/types";
import { ParamFields, ProfileKindPicker } from "@/features/runs/components/profile-fields";
import type { ProfileKind, ProfileParams } from "@/features/runs/types";

import { createScenario, updateScenario, type ScenarioFormState } from "../actions";
import { linesOf } from "../findings";
import { starterScript } from "../template";
import { isDirty } from "../workspace";
import type { Scenario } from "../types";
import { ScriptEditor } from "./script-editor";
import { ScriptFindings } from "./script-findings";

const field =
  "h-9 rounded-md border bg-background px-3 text-sm outline-none focus-visible:ring-2 focus-visible:ring-ring";

// A scenario stored without parameters of its own comes back with every one at
// zero, which describes no test: those are left for the user to fill in.
function ownParams(scenario: Scenario | undefined): ProfileParams | undefined {
  return scenario && scenario.default_params.target_vus >= 1 ? scenario.default_params : undefined;
}

function FieldError({ children }: { children?: string }) {
  if (!children) return null;
  return (
    <span role="alert" className="text-xs text-muted-foreground">
      {children}
    </span>
  );
}

// ScenarioForm writes a new scenario, or the next version of one. A client
// component because every field is held here: a refused script must come back
// exactly as it was typed, with the lines at fault marked.
//
// Creating starts from a script that already passes the checks. Editing shows
// the parameters as they are stored: changing the profile keeps them, since a
// scenario's defaults are its own, and each run is clamped to its environment.
export function ScenarioForm({
  projectId,
  scenario,
  endpoints,
  startAs = "code",
  onDirtyChange,
}: {
  projectId: string;
  scenario?: Scenario;
  // How a new scenario is written. A graph has no script to type: it is drawn on
  // the canvas, which it opens on once created.
  startAs?: "code" | "graph";
  // The project's catalog, offered to the script editor.
  endpoints?: CatalogEndpoint[];
  // Told whenever the form starts or stops differing from what is saved, so that
  // the bar above it can say so and hold Run back.
  onDirtyChange?: (dirty: boolean) => void;
}) {
  const submit = scenario ? updateScenario.bind(null, projectId, scenario.id) : createScenario.bind(null, projectId);
  const [state, action, pending] = useActionState<ScenarioFormState, FormData>(submit, {});

  const [name, setName] = useState(scenario?.name ?? "");
  const [script, setScript] = useState(scenario?.script ?? starterScript);
  const [profile, setProfile] = useState<ProfileKind>(scenario?.default_profile ?? "smoke");
  // Editing always sends the parameters: leaving them out would keep the
  // stored ones, which is not what "the template" would mean.
  const [custom, setCustom] = useState(scenario !== undefined);
  const [params, setParams] = useState<Partial<ProfileParams>>(
    ownParams(scenario) ?? { target_vus: 1, ramp_up_s: 0, plateau_s: 60, ramp_down_s: 0 },
  );

  // What the form started from. Held, not recomputed: an edit that is put back
  // is no edit, and a saved one makes a new form (keyed on the version) with a
  // new start.
  const [started] = useState({ name, script, profile, params });
  const dirty = scenario !== undefined && isDirty(started, { name, script, profile, params });
  useEffect(() => {
    onDirtyChange?.(dirty);
  }, [dirty, onDirtyChange]);

  const findings = state.findings ?? [];
  // A script Ballast generated from a graph is shown, not edited.
  const generated = scenario?.mode === "graph";

  return (
    <form action={action} className="flex flex-col gap-6">
      {(scenario || startAs === "graph") && <input type="hidden" name="mode" value={scenario?.mode ?? startAs} />}
      <label className="flex max-w-xl flex-col gap-1.5 text-sm">
        Name
        <input
          name="name"
          required
          maxLength={120}
          value={name}
          onChange={(event) => setName(event.target.value)}
          placeholder="checkout"
          aria-invalid={state.name ? true : undefined}
          className={field}
        />
        <FieldError>{state.name}</FieldError>
      </label>

      {scenario === undefined && startAs === "graph" ? (
        <p className="flex max-w-xl items-start gap-2 rounded-lg border bg-card px-3 py-2 text-xs text-muted-foreground">
          <Info aria-hidden className="mt-0.5 size-3.5 shrink-0" />
          <span>
            It starts from one request and its end, and opens on the canvas to be drawn from there. Ballast writes the
            script from the graph, so there is none to type.
          </span>
        </p>
      ) : (
      <div className="flex flex-col gap-2 text-sm">
        <span>Script</span>
        <p className="flex items-start gap-2 rounded-lg border bg-card px-3 py-2 text-xs text-muted-foreground">
          <Info aria-hidden className="mt-0.5 size-3.5 shrink-0" />
          {generated ? (
            <span>
              Ballast generated this script from the scenario&apos;s graph, so it cannot be edited here. The name and
              the default load can still change.
            </span>
          ) : (
            <span>
              Do not write an <code className="data text-foreground">options</code> block: Ballast generates it from
              the profile and the thresholds. Read the target from{" "}
              <code className="data text-foreground">__ENV.BASE_URL</code>, and give each request a{" "}
              <code className="data text-foreground">name</code> tag.
            </span>
          )}
        </p>
        <ScriptEditor
          value={script}
          onChange={setScript}
          marked={linesOf(findings)}
          invalid={findings.length > 0}
          readOnly={generated}
          endpoints={endpoints}
          importHref={`/projects/${projectId}/scenarios/import`}
        />
        <ScriptFindings findings={findings} kind="error" />
      </div>
      )}

      <fieldset className="flex flex-col gap-1.5 text-sm">
        <legend className="pb-1.5">Default profile</legend>
        <ProfileKindPicker value={profile} onChange={setProfile} />
      </fieldset>

      <fieldset className="flex max-w-xl flex-col gap-3 text-sm">
        <legend className="pb-1.5">Default parameters</legend>
        {scenario ? (
          <input type="hidden" name="custom" value="on" />
        ) : (
          <label className="flex items-center gap-2 text-muted-foreground">
            <input
              type="checkbox"
              name="custom"
              checked={custom}
              onChange={(event) => setCustom(event.target.checked)}
              className="accent-brand"
            />
            Customise — otherwise the {profile} template applies
          </label>
        )}

        {custom && (
          <ParamFields
            values={params}
            onChange={(name, value) => setParams((current) => ({ ...current, [name]: value }))}
          />
        )}
        <span className="text-xs text-muted-foreground">
          What a run starts from. Each run can change them, and they are lowered to the environment&apos;s ceilings
          before it is queued.
        </span>
        <FieldError>{state.params}</FieldError>
      </fieldset>

      {state.error && (
        <p role="alert" className="rounded-lg border bg-card px-3 py-2 text-sm text-muted-foreground">
          {state.error}
        </p>
      )}
      {state.saved && findings.length === 0 && (
        <p role="status" className="text-sm text-muted-foreground">
          Saved.
        </p>
      )}

      <div className="flex gap-2">
        <Button type="submit" disabled={pending}>
          <Save aria-hidden />
          {pending ? "Saving…" : scenario ? "Save changes" : "Create scenario"}
        </Button>
        {!scenario && (
          <Link href={`/projects/${projectId}/scenarios`} className={buttonVariants({ variant: "ghost" })}>
            Cancel
          </Link>
        )}
      </div>
    </form>
  );
}
