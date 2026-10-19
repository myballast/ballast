"use client";

import { Play } from "lucide-react";
import { useActionState, useState } from "react";

import { Button } from "@/components/ui/button";
import { verificationLabel } from "@/features/environments/components/verification-status";
import type { Environment } from "@/features/environments/types";
import type { ScenarioSummary } from "@/features/scenarios/types";

import { launchRun, type LaunchState } from "../actions";
import { profileKinds, type ProfileKind, type ProfileParams } from "../types";

const field =
  "h-9 rounded-md border bg-background px-3 text-sm outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:opacity-50";

// A scenario stored without parameters comes back with every one at zero,
// which describes no runnable test. That is "no parameters of its own", and
// the profile's template applies, as it would for any other profile.
function ownParams(scenario: ScenarioSummary | undefined): ProfileParams | undefined {
  return scenario && scenario.default_params.target_vus >= 1 ? scenario.default_params : undefined;
}

const paramFields: { name: keyof ProfileParams; label: string; unit: string }[] = [
  { name: "target_vus", label: "Virtual users", unit: "VUs" },
  { name: "ramp_up_s", label: "Ramp-up", unit: "s" },
  { name: "plateau_s", label: "Plateau", unit: "s" },
  { name: "ramp_down_s", label: "Ramp-down", unit: "s" },
];

// LaunchForm picks a scenario, a verified environment, a profile and its
// parameters, and queues the run. A client component because each choice
// shapes the next: the scenario's own profile and parameters are offered first,
// and the environment's caps are shown next to the parameters they cap.
//
// Without custom parameters the API applies the profile's template. The
// scenario's own parameters are therefore sent explicitly when its own profile
// is kept, which is what "the scenario's defaults" has to mean.
export function LaunchForm({
  projectId,
  scenarios,
  environments,
}: {
  projectId: string;
  scenarios: ScenarioSummary[];
  environments: Environment[];
}) {
  const verified = environments.filter((e) => e.verification_status === "verified");

  const [scenarioId, setScenarioId] = useState(scenarios[0]?.id ?? "");
  const [environmentId, setEnvironmentId] = useState(verified[0]?.id ?? "");
  const scenario = scenarios.find((s) => s.id === scenarioId);
  const environment = environments.find((e) => e.id === environmentId);

  const [profile, setProfile] = useState<ProfileKind>(scenario?.default_profile ?? "smoke");
  const [custom, setCustom] = useState(ownParams(scenario) !== undefined);
  const [params, setParams] = useState<ProfileParams | undefined>(ownParams(scenario));

  const [state, action, pending] = useActionState<LaunchState, FormData>(launchRun.bind(null, projectId), {});

  // Choosing a scenario, or going back to its profile, brings its own
  // parameters back. Another profile starts from that profile's template.
  const choose = (next: ScenarioSummary | undefined, nextProfile: ProfileKind) => {
    setProfile(nextProfile);
    const own = next !== undefined && next.default_profile === nextProfile ? ownParams(next) : undefined;
    setCustom(own !== undefined);
    if (own) setParams(own);
  };

  return (
    <form action={action} className="flex max-w-xl flex-col gap-6">
      <label className="flex flex-col gap-1.5 text-sm">
        Scenario
        <select
          name="scenario_id"
          value={scenarioId}
          onChange={(event) => {
            const next = scenarios.find((s) => s.id === event.target.value);
            setScenarioId(event.target.value);
            choose(next, next?.default_profile ?? profile);
          }}
          className={field}
        >
          {scenarios.map((s) => (
            <option key={s.id} value={s.id}>
              {s.name} (v{s.version})
            </option>
          ))}
        </select>
      </label>

      <label className="flex flex-col gap-1.5 text-sm">
        Environment
        <select
          name="environment_id"
          value={environmentId}
          onChange={(event) => setEnvironmentId(event.target.value)}
          className={field}
        >
          {environments.map((e) => (
            // Only a verified environment takes load. The others are listed,
            // greyed out, with the reason, rather than left to be wondered about.
            <option key={e.id} value={e.id} disabled={e.verification_status !== "verified"}>
              {e.name}
              {e.verification_status !== "verified" && ` — ${verificationLabel(e.verification_status)}`}
            </option>
          ))}
        </select>
        {environment && <span className="data truncate text-xs text-muted-foreground">{environment.base_url}</span>}
      </label>

      <fieldset className="flex flex-col gap-1.5 text-sm">
        <legend className="pb-1.5">Profile</legend>
        <div className="flex flex-wrap gap-2">
          {profileKinds.map((kind) => (
            <label
              key={kind}
              className="flex cursor-pointer items-center gap-1.5 rounded-md border px-3 py-1.5 has-[:focus-visible]:ring-2 has-[:focus-visible]:ring-ring has-[:checked]:border-brand has-[:checked]:text-foreground text-muted-foreground"
            >
              <input
                type="radio"
                name="profile"
                value={kind}
                checked={profile === kind}
                onChange={() => choose(scenario, kind)}
                className="accent-brand"
              />
              {kind}
            </label>
          ))}
        </div>
      </fieldset>

      <fieldset className="flex flex-col gap-3 text-sm">
        <legend className="pb-1.5">Parameters</legend>
        <label className="flex items-center gap-2 text-muted-foreground">
          <input
            type="checkbox"
            name="custom"
            checked={custom}
            onChange={(event) => setCustom(event.target.checked)}
            className="accent-brand"
          />
          {scenario && profile === scenario.default_profile && ownParams(scenario)
            ? "Use these parameters (the scenario's own, editable)"
            : `Customise — otherwise the ${profile} template applies`}
        </label>

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
                    value={params?.[f.name] ?? ""}
                    onChange={(event) =>
                      setParams((current) => ({
                        ...(current ?? { target_vus: 1, ramp_up_s: 0, plateau_s: 60, ramp_down_s: 0 }),
                        [f.name]: Number.parseInt(event.target.value, 10),
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

        {environment && (
          <p className="text-xs text-muted-foreground">
            {environment.name} takes up to <span className="num">{environment.max_vus}</span> VUs for{" "}
            <span className="num">{environment.max_duration_s}</span> s. Anything above is lowered to fit before the
            run is queued.
            {environment.allowed_window &&
              ` It takes load from ${environment.allowed_window.start} to ${environment.allowed_window.end} (${environment.allowed_window.timezone}) only, for the whole run.`}
          </p>
        )}
      </fieldset>

      {state.error && (
        <p role="alert" className="rounded-lg border bg-card px-3 py-2 text-sm text-muted-foreground">
          {state.error}
        </p>
      )}

      <Button type="submit" disabled={pending || !environmentId || !scenarioId} className="self-start">
        <Play aria-hidden />
        {pending ? "Launching…" : "Launch run"}
      </Button>
    </form>
  );
}
