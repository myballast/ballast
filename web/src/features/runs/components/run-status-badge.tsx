import {
  Ban,
  CircleCheck,
  CircleDashed,
  CircleSlash,
  CircleX,
  Clock,
  LoaderCircle,
  TriangleAlert,
  type LucideIcon,
} from "lucide-react";

import { StatusBadge, type StatusTone } from "@/components/status-badge";

import type { RunStatus } from "../types";

// Every status is shown with an icon and a word, never with a colour alone.
const statuses: Record<
  RunStatus,
  { label: string; icon: LucideIcon; tone: StatusTone; spin?: boolean }
> = {
  queued: { label: "Queued", icon: CircleDashed, tone: "neutral" },
  running: {
    label: "Running",
    icon: LoaderCircle,
    tone: "neutral",
    spin: true,
  },
  passed: { label: "Passed", icon: CircleCheck, tone: "passed" },
  failed: { label: "Failed", icon: CircleX, tone: "failed" },
  errored: { label: "Errored", icon: TriangleAlert, tone: "failed" },
  timeout: { label: "Timed out", icon: Clock, tone: "warning" },
  cancelled: { label: "Cancelled", icon: CircleSlash, tone: "neutral" },
  rejected: { label: "Rejected", icon: Ban, tone: "warning" },
  skipped: { label: "Skipped", icon: CircleSlash, tone: "neutral" },
  // Not an ending: the reaper still has to clean up behind it.
  orphaned: { label: "Orphaned", icon: TriangleAlert, tone: "warning" },
};

// The word a status is said with, for a place that has no room for the badge:
// the options of a filter.
export function runStatusLabel(status: RunStatus): string {
  return statuses[status].label;
}

// RunStatusIcon is the status of a run as its icon alone, for a title that has
// the badge next to it. It carries the word for a screen reader.
export function RunStatusIcon({ status }: { status: RunStatus }) {
  const { label, icon, tone, spin } = statuses[status];
  return <StatusBadge tone={tone} icon={icon} label={label} spin={spin} variant="icon" className="[&>svg]:size-[18px]" />;
}

export function RunStatusBadge({ status }: { status: RunStatus }) {
  const { label, icon, tone, spin } = statuses[status];
  return <StatusBadge tone={tone} icon={icon} label={label} spin={spin} />;
}
