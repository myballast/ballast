import { Clock } from "lucide-react";

import type { Environment } from "../types";

export function formatCap(seconds: number): string {
  return seconds % 60 === 0 ? `${seconds / 60} min` : `${seconds} s`;
}

// Ceilings is what every run against the environment is clamped to, and the
// hours it takes load, if it does not take it at any hour.
export function Ceilings({ environment: e }: { environment: Environment }) {
  return (
    <span className="flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-muted-foreground">
      <span>
        up to <span className="num">{e.max_vus}</span> VUs for <span className="num">{formatCap(e.max_duration_s)}</span>
      </span>
      {e.allowed_window && (
        <span className="flex items-center gap-1">
          <Clock aria-hidden className="size-3.5" />
          <span className="num">
            {e.allowed_window.start}–{e.allowed_window.end}
          </span>{" "}
          {e.allowed_window.timezone}
        </span>
      )}
    </span>
  );
}
