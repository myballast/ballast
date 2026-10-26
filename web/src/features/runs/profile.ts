import type { ProfileParams } from "./types.ts";

// The arithmetic of a load profile, as the page needs it: what it is made of, how
// long it lasts, and what it will be lowered to. The API is the one that does it
// when a run is queued (internal/domain/profile.go); this says it beforehand, and
// both are tested against the same list (internal/domain/testdata/clamp.json).

export type Stage = {
  name: string;
  seconds: number;
  from: number;
  to: number;
};

// stagesOf is what k6 will be told to do, leg by leg. With no ramp at either end the
// load is flat and one stage says so; otherwise a climb, a plateau and a descent,
// each only when it lasts.
export function stagesOf(params: ProfileParams): Stage[] {
  const { target_vus: vus, ramp_up_s: up, plateau_s: plateau, ramp_down_s: down } = params;

  if (up === 0 && down === 0) return plateau > 0 ? [{ name: "steady", seconds: plateau, from: vus, to: vus }] : [];

  const stages: Stage[] = [];
  if (up > 0) stages.push({ name: "ramp up", seconds: up, from: 0, to: vus });
  if (plateau > 0) stages.push({ name: "plateau", seconds: plateau, from: vus, to: vus });
  if (down > 0) stages.push({ name: "ramp down", seconds: down, from: vus, to: 0 });
  return stages;
}

export function totalSeconds(params: ProfileParams): number {
  return params.ramp_up_s + params.plateau_s + params.ramp_down_s;
}

// duration writes seconds as a person reads them: 45s, 2m 00s, 1h 05m.
export function duration(seconds: number): string {
  if (seconds < 60) return `${seconds}s`;
  const hours = Math.floor(seconds / 3600);
  const minutes = Math.floor((seconds % 3600) / 60);
  const rest = seconds % 60;
  if (hours > 0) return `${hours}h ${String(minutes).padStart(2, "0")}m`;
  return `${minutes}m ${String(rest).padStart(2, "0")}s`;
}

// summaryOf is the line at the top of the dock: "3 stages · peak 100 VUs · 2m 00s".
export function summaryOf(params: ProfileParams): string {
  const stages = stagesOf(params).length;
  const vus = params.target_vus;
  return `${stages} ${stages === 1 ? "stage" : "stages"} · peak ${vus} ${vus === 1 ? "VU" : "VUs"} · ${duration(totalSeconds(params))}`;
}

export type Ceilings = { vus: number; seconds: number };

// clampProfile lowers a profile until it fits the ceilings, the way the API does:
// the virtual users cap at the ceiling, and the seconds are spent from a budget in
// order — the climb first, then the plateau, then the descent — so that what is
// sacrificed is the end. A test that never reaches its load measures nothing.
// It only ever lowers.
export function clampProfile(params: ProfileParams, ceilings: Ceilings): ProfileParams {
  let budget = ceilings.seconds;
  const spend = (seconds: number) => {
    const taken = Math.min(seconds, budget);
    budget -= taken;
    return taken;
  };

  const ramp_up_s = spend(params.ramp_up_s);
  const plateau_s = spend(params.plateau_s);
  const ramp_down_s = spend(params.ramp_down_s);

  return { target_vus: Math.min(params.target_vus, ceilings.vus), ramp_up_s, plateau_s, ramp_down_s };
}

export function isLowered(asked: ProfileParams, given: ProfileParams): boolean {
  return (
    asked.target_vus !== given.target_vus ||
    asked.ramp_up_s !== given.ramp_up_s ||
    asked.plateau_s !== given.plateau_s ||
    asked.ramp_down_s !== given.ramp_down_s
  );
}

// paramsProblem says why these parameters describe no runnable test, in a sentence,
// or nothing. The same rules as ProfileParams.Validate in the API.
export function paramsProblem(params: Partial<ProfileParams>): string | undefined {
  const { target_vus: vus, ramp_up_s: up, plateau_s: plateau, ramp_down_s: down } = params;
  if ([vus, up, plateau, down].some((n) => n === undefined || !Number.isInteger(n))) return "Every parameter must be a whole number.";
  if ((vus as number) < 1) return "At least one virtual user.";
  if ((up as number) < 0 || (plateau as number) < 0 || (down as number) < 0) return "A duration cannot be negative.";
  if ((up as number) + (plateau as number) + (down as number) < 1) return "The run would last no time at all.";
  return undefined;
}

// sameProfile: whether two profiles are the same one, field for field.
export function sameParams(a: Partial<ProfileParams>, b: Partial<ProfileParams>): boolean {
  return a.target_vus === b.target_vus && a.ramp_up_s === b.ramp_up_s && a.plateau_s === b.plateau_s && a.ramp_down_s === b.ramp_down_s;
}
