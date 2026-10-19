import type { ProfileKind, ProfileParams } from "@/features/runs/types";

// "smoke · 1 VU · 60 s": the default profile of a scenario in the few words the
// top bar has room for. The duration is the whole of the profile — ramp up,
// plateau and ramp down — since that is how long a run lasts.
export function profileSummary(profile: ProfileKind, params: ProfileParams): string {
  const vus = `${params.target_vus} ${params.target_vus === 1 ? "VU" : "VUs"}`;
  const seconds = params.ramp_up_s + params.plateau_s + params.ramp_down_s;
  const length = seconds >= 120 && seconds % 60 === 0 ? `${seconds / 60} min` : `${seconds} s`;
  return `${profile} · ${vus} · ${length}`;
}

// What the form holds, as the top bar compares it with what is stored.
export type Draft = {
  name: string;
  script: string;
  profile: ProfileKind;
  params: Partial<ProfileParams>;
};

// isDirty reports whether the form differs from what was saved. A parameter left
// empty counts as a change, since it is not what is stored.
export function isDirty(saved: Draft, draft: Draft): boolean {
  if (saved.name !== draft.name || saved.script !== draft.script || saved.profile !== draft.profile) return true;

  const keys: (keyof ProfileParams)[] = ["target_vus", "ramp_up_s", "plateau_s", "ramp_down_s"];
  return keys.some((key) => saved.params[key] !== draft.params[key]);
}

// Why Run cannot be pressed, in a sentence, or null when it can. A run always
// executes a saved version, so changes that were not saved are not what it would
// run — and the button says so instead of quietly running the old one.
export function runBlockedReason(input: {
  dirty: boolean;
  canRun: boolean;
  verifiedEnvironments: number;
  pending?: boolean;
}): string | null {
  if (!input.canRun) return "Your role in this project does not include launching runs.";
  if (input.dirty) return "Save your changes first: a run always executes a saved version of the script.";
  if (input.verifiedEnvironments === 0) {
    return "No environment is verified yet. Load is only sent to a target whose ownership is proved and approved.";
  }
  return null;
}
