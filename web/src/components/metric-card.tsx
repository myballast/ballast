import { TriangleAlert, type LucideIcon } from "lucide-react";

import { InfoTip } from "@/components/info-tip";
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
  help,
  notice,
  status,
  closeness,
}: {
  label: string;
  value: string;
  // A line under the value, such as the threshold that applies to it.
  hint?: string;
  // What the number means, for somebody new to load testing: an "i" next to the label.
  help?: string;
  // A warning about the number, in words, under it. Never a status colour.
  notice?: string | null;
  status?: { tone: StatusTone; icon: LucideIcon; label: string };
  // How near the number is to its threshold, from 0 to 1. A bar in a neutral tone:
  // the verdict is the badge's, with its word, and a colour is not drawn twice.
  closeness?: number | null;
}) {
  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-1.5 text-[11px] font-normal tracking-wider text-muted-foreground uppercase">
          {label}
          {help && <InfoTip label={label}>{help}</InfoTip>}
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
        {closeness != null && (
          <div aria-hidden className="h-1 overflow-hidden rounded-full bg-muted">
            <div className="h-full rounded-full bg-muted-foreground/60" style={{ width: `${Math.round(closeness * 100)}%` }} />
          </div>
        )}
        {notice && (
          <p className="flex items-start gap-1.5 text-xs text-muted-foreground">
            <TriangleAlert aria-hidden className="mt-0.5 size-3.5 shrink-0" />
            {notice}
          </p>
        )}
      </CardContent>
    </Card>
  );
}
