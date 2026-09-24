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

import { Badge } from "@/components/ui/badge";
import { cn } from "@/lib/utils";

import type { RunStatus } from "../types";

type Tone = "passed" | "warning" | "failed" | "neutral";

// Every status is shown with an icon and a word, never with a colour alone.
const statuses: Record<
  RunStatus,
  { label: string; icon: LucideIcon; tone: Tone; spin?: boolean }
> = {
  queued: { label: "Queued", icon: CircleDashed, tone: "neutral" },
  running: { label: "Running", icon: LoaderCircle, tone: "neutral", spin: true },
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

const tones: Record<Tone, string> = {
  passed: "border-status-passed/35 bg-status-passed/10 text-status-passed",
  warning: "border-status-warning/35 bg-status-warning/10 text-status-warning",
  failed: "border-status-failed/35 bg-status-failed/10 text-status-failed",
  neutral: "border-border bg-muted text-muted-foreground",
};

export function RunStatusBadge({ status }: { status: RunStatus }) {
  const { label, icon: Icon, tone, spin } = statuses[status];

  return (
    <Badge variant="outline" className={cn("h-6 px-2.5", tones[tone])}>
      <Icon aria-hidden className={cn(spin && "animate-spin")} />
      {label}
    </Badge>
  );
}
