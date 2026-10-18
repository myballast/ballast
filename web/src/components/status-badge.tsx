import type { LucideIcon } from "lucide-react";

import { Badge } from "@/components/ui/badge";
import { cn } from "@/lib/utils";

// The tone of a status. Green, amber and red exist for this and nothing else:
// this file is the only one allowed to use them (`npm run lint` checks).
export type StatusTone = "passed" | "warning" | "failed" | "neutral";

const pill: Record<StatusTone, string> = {
  passed: "border-status-passed/35 bg-status-passed-bg text-status-passed",
  warning: "border-status-warning/35 bg-status-warning-bg text-status-warning",
  failed: "border-status-failed/35 bg-status-failed-bg text-status-failed",
  neutral: "border-border bg-muted text-muted-foreground",
};

const inline: Record<StatusTone, string> = {
  passed: "text-status-passed",
  warning: "text-status-warning",
  failed: "text-status-failed",
  neutral: "text-muted-foreground",
};

// StatusBadge shows the state of a run or a threshold. The icon and the label
// are required: a colour alone says nothing to whoever cannot tell green from
// red, or reads the page on a grey screen.
export function StatusBadge({
  tone,
  icon: Icon,
  label,
  spin = false,
  variant = "pill",
  className,
}: {
  tone: StatusTone;
  icon: LucideIcon;
  label: string;
  spin?: boolean;
  // A pill stands on its own; inline sits in a row of text, like a list; an
  // icon is the icon alone, for a place that already says the word beside it,
  // and still says it to a screen reader.
  variant?: "pill" | "inline" | "icon";
  className?: string;
}) {
  const icon = (
    <Icon
      aria-hidden
      className={cn(variant === "inline" && "size-3.5", spin && "animate-spin")}
    />
  );

  if (variant === "icon") {
    return (
      <span role="img" aria-label={label} className={cn("inline-flex", inline[tone], className)}>
        {icon}
      </span>
    );
  }

  if (variant === "inline") {
    return (
      <span
        className={cn(
          "inline-flex items-center gap-1.5 text-xs",
          inline[tone],
          className,
        )}
      >
        {icon}
        {label}
      </span>
    );
  }

  return (
    <Badge
      variant="outline"
      className={cn("h-6 px-2.5", pill[tone], className)}
    >
      {icon}
      {label}
    </Badge>
  );
}
