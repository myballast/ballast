import type { ProfileKind } from "./types.ts";

// What each profile is for, in words for somebody who has never done a load test.
// It says the shape and the purpose, never a number: the numbers are the template
// the API holds (internal/domain/profile.go), and a second copy here would drift.
export type ProfileHelp = {
  summary: string;
  when: string;
  // The load over time, as points from 0 to 1 on both axes. A drawing of the idea,
  // not of the parameters.
  shape: [number, number][];
};

export const profileHelp: Record<ProfileKind, ProfileHelp> = {
  smoke: {
    summary: "A quick sanity check with very little load: does the scenario work at all?",
    when: "After every change, before anything heavier.",
    shape: [
      [0, 0.15],
      [1, 0.15],
    ],
  },
  load: {
    summary: "The traffic you expect on a normal busy day, climbed to gently and held.",
    when: "Before a release, to know it holds its usual load.",
    shape: [
      [0, 0],
      [0.15, 0.55],
      [0.85, 0.55],
      [1, 0],
    ],
  },
  stress: {
    summary: "More than you expect, climbed to slowly, to find where the target starts to struggle.",
    when: "To size the target, or to learn its limit.",
    shape: [
      [0, 0],
      [0.75, 1],
      [0.9, 1],
      [1, 0],
    ],
  },
  spike: {
    summary: "A sudden jump in traffic, then back to calm: how does the target cope, and recover?",
    when: "Before a launch, a sale or a mailing.",
    shape: [
      [0, 0.05],
      [0.35, 0.05],
      [0.4, 1],
      [0.7, 1],
      [0.75, 0.05],
      [1, 0.05],
    ],
  },
  soak: {
    summary: "A moderate, steady load for a long time, to find slow leaks and wear.",
    when: "To catch memory leaks or connections that pile up.",
    shape: [
      [0, 0],
      [0.05, 0.4],
      [0.95, 0.4],
      [1, 0],
    ],
  },
};
