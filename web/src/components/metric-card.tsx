import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";

// MetricCard shows one measured number. The value is always set in the
// monospace face with tabular figures, so that numbers line up from one card
// to the next. It carries no status colour: whether a number is good is a
// threshold's verdict, shown by a StatusBadge.
export function MetricCard({
  label,
  value,
  hint,
}: {
  label: string;
  value: string;
  // A line under the value, such as the threshold that applies to it.
  hint?: string;
}) {
  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-[11px] font-normal tracking-wider text-muted-foreground uppercase">
          {label}
        </CardTitle>
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
