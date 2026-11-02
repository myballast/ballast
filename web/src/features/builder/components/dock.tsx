"use client";

import { CircleAlert, Save } from "lucide-react";
import { useId, useState, useTransition } from "react";

import { Button } from "@/components/ui/button";
import { ParamFields, ProfileKindPicker } from "@/features/runs/components/profile-fields";
import { clampProfile, duration, isLowered, paramsProblem, stagesOf, summaryOf, totalSeconds } from "@/features/runs/profile";
import type { ProfileKind, ProfileParams } from "@/features/runs/types";
import { ThresholdRules } from "@/features/scenarios/components/threshold-rules";
import type { EndpointChoice } from "@/features/scenarios/threshold-presets";
import type { Threshold } from "@/features/scenarios/types";
import { cn } from "@/lib/utils";

import { saveProfile } from "../actions";

export type Target = {
  id: string;
  name: string;
  // What a run can really use on it, as the API says it.
  effectiveVus: number;
  effectiveSeconds: number;
};

export type ProfileDraft = { kind: ProfileKind; params: Partial<ProfileParams> };

type Props = {
  projectId: string;
  scenarioId: string;
  thresholds: Threshold[];
  // The requests of the graph, for a rule narrowed to one of them.
  endpoints: EndpointChoice[];
  canEdit: boolean;
  // The profile as it is being edited, and whether that differs from what is saved.
  draft: ProfileDraft;
  onDraft: (draft: ProfileDraft) => void;
  dirty: boolean;
  // Whether the scenario has parameters of its own; a legacy one stored with none is
  // started from a neutral profile.
  hasOwnParams: boolean;
  // The environment the bar has picked, whose ceilings are recalled and applied.
  target: Target | undefined;
};

// Dock is what sits under the canvas: the load profile, and the thresholds that
// judge the run. The fields are the ones the scenario's page and the launch form
// use, and the thresholds are the scenario page's editor as it is — the dock holds
// no copy of either.
export function Dock(props: Props) {
  const { thresholds, draft } = props;
  const [tab, setTab] = useState<"profile" | "thresholds">("profile");

  const complete = paramsProblem(draft.params) === undefined;

  // The line at the top says what the run will be: the profile as it is lowered to
  // the environment picked, with a word when it was.
  const asked = draft.params as ProfileParams;
  const given = complete && props.target ? clampProfile(asked, { vus: props.target.effectiveVus, seconds: props.target.effectiveSeconds }) : asked;
  const summary = complete
    ? `${summaryOf(given)}${isLowered(asked, given) ? " · lowered to fit" : ""}`
    : "Fill in the parameters to see what the profile is made of.";

  const tabs = [
    { id: "profile" as const, label: "Load profile", count: complete ? stagesOf(given).length : 0 },
    { id: "thresholds" as const, label: "Thresholds", count: thresholds.length },
  ];

  return (
    <section aria-label="Load profile and thresholds" className="flex flex-col rounded-xl border bg-card">
      <div className="flex flex-wrap items-center gap-x-6 gap-y-1 border-b px-3">
        <div role="tablist" aria-label="Dock" className="flex gap-4">
          {tabs.map((t) => (
            <button
              key={t.id}
              type="button"
              role="tab"
              id={`dock-tab-${t.id}`}
              aria-selected={tab === t.id}
              aria-controls={`dock-panel-${t.id}`}
              onClick={() => setTab(t.id)}
              className={cn(
                "flex h-10 items-center gap-2 border-b-2 border-transparent text-sm text-muted-foreground",
                "focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none",
                tab === t.id && "border-brand text-foreground",
              )}
            >
              {t.label}
              <span className="data rounded-md border px-1.5 text-[10px]">{t.count}</span>
            </button>
          ))}
        </div>
        <p role="status" className="data ml-auto py-2 text-xs text-muted-foreground">
          {tab === "profile"
            ? summary
            : `${thresholds.length} ${thresholds.length === 1 ? "threshold" : "thresholds"}`}
        </p>
      </div>

      {tab === "profile" ? (
        <div role="tabpanel" id="dock-panel-profile" aria-labelledby="dock-tab-profile" className="p-3">
          <ProfileTab {...props} />
        </div>
      ) : (
        <div role="tabpanel" id="dock-panel-thresholds" aria-labelledby="dock-tab-thresholds" className="p-3">
          <ThresholdRules
            projectId={props.projectId}
            scenarioId={props.scenarioId}
            thresholds={thresholds}
            editable={props.canEdit}
            endpoints={props.endpoints}
          />
        </div>
      )}
    </section>
  );
}

function ProfileTab({ projectId, scenarioId, canEdit, draft, onDraft, dirty, hasOwnParams, target }: Props) {
  const [pending, startTransition] = useTransition();
  const [message, setMessage] = useState<string | null>(null);
  const problem = paramsProblem(draft.params);
  const complete = problem === undefined;
  const params = draft.params as ProfileParams;

  const given = complete && target ? clampProfile(params, { vus: target.effectiveVus, seconds: target.effectiveSeconds }) : null;
  const lowered = complete && given !== null && isLowered(params, given);

  function save() {
    if (!complete) return;
    setMessage(null);
    startTransition(async () => {
      const outcome = await saveProfile(projectId, scenarioId, draft.kind, params);
      setMessage(outcome.error ?? "Saved. It is the next version of the scenario.");
    });
  }

  const shown = complete ? (given ?? params) : null;
  const ceiling = target ? target.effectiveVus : null;
  const peak = shown ? shown.target_vus : 0;
  const ramps = complete && (params.ramp_up_s > 0 || params.ramp_down_s > 0);

  return (
    <div className="grid gap-4 lg:grid-cols-[13rem_minmax(0,1fr)_minmax(0,20rem)]">
      <dl className="flex flex-col gap-3 text-sm">
        <div className="flex flex-col gap-1">
          <dt className="text-[11px] tracking-wider text-muted-foreground uppercase">Executor</dt>
          <dd className="data text-xs">{ramps ? "ramping virtual users" : "constant virtual users"}</dd>
        </div>
        <div className="flex flex-col gap-1">
          <dt className="text-[11px] tracking-wider text-muted-foreground uppercase">Duration</dt>
          <dd className="data text-xs">{shown ? duration(totalSeconds(shown)) : "—"}</dd>
        </div>
        <div className="flex flex-col gap-1.5">
          <dt className="text-[11px] tracking-wider text-muted-foreground uppercase">Peak load</dt>
          <dd className="flex flex-col gap-1.5">
            <span className="data text-xs">
              {shown ? peak : "—"}
              {ceiling !== null && <span className="text-muted-foreground"> / {ceiling} allowed</span>}
            </span>
            {ceiling !== null && shown && (
              <span aria-hidden className="h-1.5 overflow-hidden rounded-full bg-muted">
                <span className="block h-full rounded-full bg-brand" style={{ width: `${Math.min(100, (peak / ceiling) * 100)}%` }} />
              </span>
            )}
          </dd>
        </div>
        <p className="text-xs text-muted-foreground">
          {target ? (
            <>
              <span className="text-foreground">{target.name}</span> caps runs at{" "}
              <span className="num text-foreground">{target.effectiveVus}</span> VUs and{" "}
              <span className="num text-foreground">{duration(target.effectiveSeconds)}</span>.
            </>
          ) : (
            "No verified environment is picked, so there is no ceiling to recall."
          )}
          {lowered && given && (
            <>
              {" "}
              This profile will run at <span className="num text-foreground">{given.target_vus}</span> VUs for{" "}
              <span className="num text-foreground">{duration(totalSeconds(given))}</span>, lowered from{" "}
              <span className="num">{params.target_vus}</span> VUs for <span className="num">{duration(totalSeconds(params))}</span>:
              the run is written with what it will do.
            </>
          )}
          {complete && target && !lowered && " It fits."}
        </p>
      </dl>

      <div className="flex flex-col gap-4">
        <fieldset className="flex flex-col gap-1.5 text-sm" disabled={!canEdit}>
          <legend className="pb-1.5">Default profile</legend>
          <ProfileKindPicker
            name="dock-profile"
            value={draft.kind}
            disabled={!canEdit}
            onChange={(kind) => onDraft({ ...draft, kind })}
          />
        </fieldset>

        <fieldset className="flex flex-col gap-2 text-sm" disabled={!canEdit}>
          <legend className="pb-1.5">Stages</legend>
          <ParamFields
            values={draft.params}
            disabled={!canEdit}
            onChange={(name, value) => onDraft({ ...draft, params: { ...draft.params, [name]: value } })}
          />
          {!hasOwnParams && !dirty && (
            <p className="text-xs text-muted-foreground">
              This scenario has no parameters of its own: a run starts from the {draft.kind} template. Save these to
              give it some.
            </p>
          )}
        </fieldset>

        {problem && (
          <p role="alert" className="flex items-start gap-1 text-xs text-muted-foreground">
            <CircleAlert aria-hidden className="mt-0.5 size-3 shrink-0" />
            {problem}
          </p>
        )}

        {canEdit && (
          <div className="flex flex-wrap items-center gap-3">
            <Button type="button" variant="outline" onClick={save} disabled={!dirty || !complete || pending}>
              <Save aria-hidden />
              {pending ? "Saving…" : "Save profile"}
            </Button>
            {dirty && <span className="text-xs text-muted-foreground">Unsaved profile: a run still uses the saved one.</span>}
            {message && (
              <span role="status" className="text-xs text-muted-foreground">
                {message}
              </span>
            )}
          </div>
        )}
      </div>

      <Stages params={shown} asked={complete ? params : null} />
    </div>
  );
}

// Stages draws the profile — each leg, and the shape of the load over time — for
// what it will really be: the profile lowered to the ceilings.
function Stages({ params, asked }: { params: ProfileParams | null; asked: ProfileParams | null }) {
  const id = useId();
  if (!params || !asked) return <div aria-hidden />;

  const stages = stagesOf(params);
  const total = Math.max(1, totalSeconds(params));
  const peak = Math.max(1, params.target_vus);
  const width = 288;
  const height = 88;

  // The line starts where the first leg does, and each leg ends at its own time.
  let at = 0;
  const points: [number, number][] = [];
  for (const stage of stages) {
    if (points.length === 0) points.push([0, stage.from]);
    at += stage.seconds;
    points.push([at, stage.to]);
  }
  const line = points
    .map(([seconds, vus]) => `${(seconds / total) * width},${height - 6 - (vus / peak) * (height - 14)}`)
    .join(" ");

  const description = stages
    .map((s) => `${s.name}, ${duration(s.seconds)}, ${s.from === s.to ? `${s.to} VUs` : `${s.from} to ${s.to} VUs`}`)
    .join("; ");

  return (
    <div className="flex flex-col gap-2">
      <svg role="img" aria-labelledby={`${id}-title`} viewBox={`0 0 ${width} ${height}`} className="w-full rounded-lg border bg-background">
        <title id={`${id}-title`}>{`Load over time: ${description}`}</title>
        <polygon points={`0,${height - 6} ${line} ${width},${height - 6}`} fill="currentColor" fillOpacity="0.14" className="text-brand" />
        <polyline points={line} fill="none" stroke="currentColor" strokeWidth="1.5" className="text-brand" />
      </svg>
      <ol className="flex flex-col divide-y rounded-lg border text-xs">
        {stages.map((stage, index) => (
          <li key={stage.name} className="data flex items-baseline gap-2 px-2.5 py-1.5">
            <span className="w-4 text-muted-foreground">{index + 1}</span>
            <span className="flex-1">{stage.name}</span>
            <span className="text-muted-foreground">{duration(stage.seconds)}</span>
            <span className="w-20 text-right">
              {stage.from === stage.to ? stage.to : `${stage.from} → ${stage.to}`} VUs
            </span>
          </li>
        ))}
      </ol>
    </div>
  );
}
