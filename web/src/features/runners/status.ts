import type { Runner, RunnerStatus } from "./types.ts";

const labels: Record<RunnerStatus, string> = {
  online: "Online",
  draining: "Draining",
  offline: "Offline",
};

export function statusLabel(status: RunnerStatus): string {
  return labels[status];
}

function plural(count: number, unit: string): string {
  return `${count} ${unit}${count === 1 ? "" : "s"} ago`;
}

// since says how long ago a runner last spoke. The runner beats every ten
// seconds, so anything under five is "just now": a precision the beat does not
// have would be a number that flickers.
//
// now is passed in rather than read, so that the server and a test agree on it.
// A beat that seems to come from the future — the two clocks differ by a few
// seconds — is a fresh one.
export function since(iso: string, now: Date): string {
  const seconds = Math.floor((now.getTime() - new Date(iso).getTime()) / 1000);

  if (seconds < 5) return "just now";
  if (seconds < 60) return plural(seconds, "second");

  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return plural(minutes, "minute");

  const hours = Math.floor(minutes / 60);
  if (hours < 24) return plural(hours, "hour");

  return plural(Math.floor(hours / 24), "day");
}

// outdatedNote is what a person reads next to a runner that is below the
// minimum. It names both numbers, because a refusal that does not is a puzzle,
// and the person reading it is the one who has to fix it.
export function outdatedNote(runner: Pick<Runner, "outdated" | "contract">, minimum: number): string | undefined {
  if (!runner.outdated) return undefined;
  return `Speaks contract ${runner.contract}, below the minimum of ${minimum}: it takes no run until it is updated.`;
}
