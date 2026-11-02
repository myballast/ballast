"use client";

import { InfoTip } from "@/components/info-tip";

import { profileHelp } from "../profile-help";
import { profileKinds, type ProfileKind, type ProfileParams } from "../types";

// The fields of a load profile, written once. The scenario form, the launch form and
// the builder's dock all ask for the same thing — which profile, and its four
// numbers — and a fourth copy of them would be a fourth place for them to drift.

export const fieldClass =
  "h-9 rounded-md border bg-background px-3 text-sm outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:opacity-50";

export const paramFields: { name: keyof ProfileParams; label: string; unit: string }[] = [
  { name: "target_vus", label: "Virtual users", unit: "VUs" },
  { name: "ramp_up_s", label: "Ramp-up", unit: "s" },
  { name: "plateau_s", label: "Plateau", unit: "s" },
  { name: "ramp_down_s", label: "Ramp-down", unit: "s" },
];

// ProfileKindPicker is the five kinds, as chips that are radio buttons: one choice,
// reachable by keyboard, with the focus ring on the chip and the choice said by a
// border and not by colour alone.
export function ProfileKindPicker({
  value,
  onChange,
  disabled = false,
  name = "profile",
}: {
  value: ProfileKind;
  onChange: (kind: ProfileKind) => void;
  disabled?: boolean;
  name?: string;
}) {
  const help = profileHelp[value];

  return (
    <div className="flex flex-col gap-3">
      <div className="flex flex-wrap gap-2">
        {profileKinds.map((kind) => (
          <div key={kind} className="flex items-center gap-1">
            <label className="flex cursor-pointer items-center gap-1.5 rounded-md border px-3 py-1.5 text-muted-foreground has-[:checked]:border-brand has-[:checked]:text-foreground has-[:disabled]:cursor-default has-[:disabled]:opacity-60 has-[:focus-visible]:ring-2 has-[:focus-visible]:ring-ring">
              <input
                type="radio"
                name={name}
                value={kind}
                checked={value === kind}
                disabled={disabled}
                onChange={() => onChange(kind)}
                className="accent-brand"
              />
              {kind}
            </label>
            <InfoTip label={`the ${kind} profile`}>{profileHelp[kind].summary}</InfoTip>
          </div>
        ))}
      </div>

      {/* What the chosen profile is for, said where it is chosen. */}
      <div aria-live="polite" className="flex items-center gap-4 rounded-lg border p-3 text-xs">
        <ProfileShape shape={help.shape} label={`${value}: ${help.summary}`} />
        <div className="flex min-w-0 flex-col gap-1">
          <p>
            <span className="text-foreground">{value}</span> — {help.summary}
          </p>
          <p className="text-muted-foreground">Use it: {help.when}</p>
        </div>
      </div>
    </div>
  );
}

// ProfileShape draws the load of a profile over time, as an idea: the real numbers
// are the parameters below it, and they can all be changed.
function ProfileShape({ shape, label }: { shape: [number, number][]; label: string }) {
  const width = 120;
  const height = 44;
  const point = ([at, level]: [number, number]) => `${at * width},${height - 4 - level * (height - 8)}`;
  const line = shape.map(point).join(" ");

  return (
    <svg role="img" aria-label={`Shape of the load over time. ${label}`} viewBox={`0 0 ${width} ${height}`} className="h-11 w-[7.5rem] shrink-0 text-brand">
      <polygon points={`0,${height - 4} ${line} ${width},${height - 4}`} fill="currentColor" fillOpacity="0.14" />
      <polyline points={line} fill="none" stroke="currentColor" strokeWidth="1.5" />
    </svg>
  );
}

// ParamFields is the four numbers. A field left empty is undefined, which is not
// zero: an empty duration is a form not finished, and the form says so.
export function ParamFields({
  values,
  onChange,
  disabled = false,
}: {
  values: Partial<ProfileParams>;
  onChange: (name: keyof ProfileParams, value: number | undefined) => void;
  disabled?: boolean;
}) {
  return (
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
              disabled={disabled}
              value={values[f.name] ?? ""}
              onChange={(event) => {
                const whole = Number.parseInt(event.target.value, 10);
                onChange(f.name, event.target.value === "" || Number.isNaN(whole) ? undefined : whole);
              }}
              className={`${fieldClass} num w-full`}
            />
            <span>{f.unit}</span>
          </span>
        </label>
      ))}
    </div>
  );
}
