"use client";

import { useSyncExternalStore } from "react";

import { formatClock, formatRelative, missing } from "@/lib/format";
import { browserZone, formatFullMoment, formatMoment } from "@/lib/time";

const nothing = () => () => {};

// Time is a moment, written once and the same everywhere: in the reader's own
// timezone, with the full date and zone in a tooltip and in the <time> element.
//
// Three ways to say it. "moment" is the date and the time to the minute; "relative"
// is how long ago ("3 hours ago"), for what is read as an age; "clock" is the time
// of day, with the day when it is not today, for the parts of a run.
//
// The server cannot know the reader's zone. It renders in UTC, and the browser
// takes over with its own on hydration — React reads the zone through
// useSyncExternalStore, so the swap is a re-render and not a mismatch. `now` is
// the server's, handed down, so that "3 hours ago" says the same on both sides.
export function Time({
  iso,
  mode = "moment",
  now,
}: {
  iso: string | undefined;
  mode?: "moment" | "relative" | "clock";
  now?: Date;
}) {
  const zone = useSyncExternalStore(nothing, browserZone, () => "UTC");

  if (!iso) return <>{missing}</>;

  const reference = now ?? new Date(iso);
  const text =
    mode === "relative"
      ? formatRelative(iso, reference)
      : mode === "clock"
        ? formatClock(iso, reference, zone)
        : formatMoment(iso, zone);

  return (
    <time dateTime={iso} title={formatFullMoment(iso, zone)}>
      {text}
    </time>
  );
}
