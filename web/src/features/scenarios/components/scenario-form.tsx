"use client";

import { Info, Save } from "lucide-react";
import Link from "next/link";
import { useActionState, useEffect, useState } from "react";

import { Button, buttonVariants } from "@/components/ui/button";
import { profileKinds, type ProfileKind, type ProfileParams } from "@/features/runs/types";

import { createScenario, updateScenario, type ScenarioFormState } from "../actions";
import { linesOf } from "../findings";
import { starterScript } from "../template";
import { isDirty } from "../workspace";
import type { Scenario } from "../types";
import { ScriptEditor } from "./script-editor";
import { ScriptFindings } from "./script-findings";

const field =
  "h-9 rounded-md border bg-background px-3 text-sm outline-none focus-visible:ring-2 focus-visible:ring-ring";

const paramFields: { name: keyof ProfileParams; label: string; unit: string }[] = [
  { name: "target_vus", label: "Virtual users", unit: "VUs" },
  { name: "ramp_up_s", label: "Ramp-up", unit: "s" },
  { name: "plateau_s", label: "Plateau", unit: "s" },
  { name: "ramp_down_s", label: "Ramp-down", unit: "s" },
];

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
  onDirtyChange,
}: {
  projectId: string;
  scenario?: Scenario;
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

  return (
    <form action={action} className="flex flex-col gap-6">
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

      <div className="flex flex-col gap-2 text-sm">
        <span>Script</span>
        <p className="flex items-start gap-2 rounded-lg border bg-card px-3 py-2 text-xs text-muted-foreground">
          <Info aria-hidden className="mt-0.5 size-3.5 shrink-0" />
          <span>
            Do not write an <code className="data text-foreground">options</code> block: Ballast generates it from the
            profile and the thresholds. Read the target from{" "}
            <code className="data text-foreground">__ENV.BASE_URL</code>, and give each request a{" "}
            <code className="data text-foreground">name</code> tag.
          </span>
        </p>
        <ScriptEditor value={script} onChange={setScript} marked={linesOf(findings)} invalid={findings.length > 0} />
        <ScriptFindings findings={findings} kind="error" />
      </div>

      <fieldset className="flex flex-col gap-1.5 text-sm">
        <legend className="pb-1.5">Default profile</legend>
        <div className="flex flex-wrap gap-2">
          {profileKinds.map((kind) => (
            <label
              key={kind}
              className="flex cursor-pointer items-center gap-1.5 rounded-md border px-3 py-1.5 text-muted-foreground has-[:focus-visible]:ring-2 has-[:focus-visible]:ring-ring has-[:checked]:border-brand has-[:checked]:text-foreground"
            >
              <input
                type="radio"
                name="profile"
                value={kind}
                checked={profile === kind}
                onChange={() => setProfile(kind)}
                className="accent-brand"
              />
              {kind}
            </label>
          ))}
        </div>
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
          <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
            {paramFields.map((f) => (
              <label key={f.name} className="flex flex-col gap-1.5 text-xs text-muted-foreground">
                {f.label}
                <span className="flex items-center gap-1.5">
                  <input
                    type="number"
                    name={f.name}
                    min={f.name === "target_vus" ? 1 : 0}
                    required
                    value={params[f.name] ?? ""}
                    onChange={(event) =>
                      setParams((current) => ({
                        ...current,
                        [f.name]: event.target.value === "" ? undefined : Number.parseInt(event.target.value, 10),
                      }))
                    }
                    className={`${field} num w-full`}
                  />
                  <span>{f.unit}</span>
                </span>
              </label>
            ))}
          </div>
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
