import type { LucideIcon } from "lucide-react";

import { StatusBadge, type StatusTone } from "@/components/status-badge";
import { Card, CardAction, CardContent, CardHeader, CardTitle } from "@/components/ui/card";

// MetricCard shows one measured number. The value is always set in the
// monospace face with tabular figures, so that numbers line up from one card
// to the next. The number itself is never coloured: whether it is good is a
// threshold's verdict, shown — when there is one — by the optional status, a
// StatusBadge with its icon and its word.
export function MetricCard({
  label,
  value,
  hint,
  status,
}: {
  label: string;
  value: string;
  // A line under the value, such as the threshold that applies to it.
  hint?: string;
  status?: { tone: StatusTone; icon: LucideIcon; label: string };
}) {
  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-[11px] font-normal tracking-wider text-muted-foreground uppercase">
          {label}
        </CardTitle>
        {status && (
          <CardAction>
            <StatusBadge variant="inline" tone={status.tone} icon={status.icon} label={status.label} />
          </CardAction>
        )}
      </CardHeader>
      <CardContent className="flex flex-col gap-1.5">
        <p className="data text-2xl">{value}</p>
        {hint && (
          <p className="data text-[11px] text-muted-foreground">{hint}</p>
        )}
      </CardContent>
    </Card>
  );
}
