"use client";

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
  return (
    <div className="flex flex-wrap gap-2">
      {profileKinds.map((kind) => (
        <label
          key={kind}
          className="flex cursor-pointer items-center gap-1.5 rounded-md border px-3 py-1.5 text-muted-foreground has-[:checked]:border-brand has-[:checked]:text-foreground has-[:disabled]:cursor-default has-[:disabled]:opacity-60 has-[:focus-visible]:ring-2 has-[:focus-visible]:ring-ring"
        >
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
      ))}
    </div>
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
