// How a moment is written on every screen: one format, in the reader's own
// timezone, with the full date a hover away.
//
// Everything here takes the zone as an argument. The browser's own is the default
// where a component runs there; a test passes one, so that what it asserts does
// not depend on the machine that runs it.
const locale = "en-GB";

const formatters = new Map<string, Intl.DateTimeFormat>();

function formatter(key: string, zone: string | undefined, options: Intl.DateTimeFormatOptions): Intl.DateTimeFormat {
  const id = `${key}|${zone ?? ""}`;
  let found = formatters.get(id);
  if (!found) {
    found = new Intl.DateTimeFormat(locale, { ...options, timeZone: zone });
    formatters.set(id, found);
  }
  return found;
}

// formatMoment is the one way a date and time are written: "18 Oct 2026, 14:02".
// To the minute, because a second is noise in a list and the full precision is in
// the tooltip.
export function formatMoment(iso: string, zone?: string): string {
  return formatter("moment", zone, {
    day: "numeric",
    month: "short",
    year: "numeric",
    hour: "2-digit",
    minute: "2-digit",
    hourCycle: "h23",
  }).format(new Date(iso));
}

// formatFullMoment is what the tooltip says: the weekday, the seconds and the
// name of the zone, so that a time is never ambiguous about whose clock it is on.
export function formatFullMoment(iso: string, zone?: string): string {
  return formatter("full", zone, {
    weekday: "long",
    day: "numeric",
    month: "long",
    year: "numeric",
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
    hourCycle: "h23",
    timeZoneName: "short",
  }).format(new Date(iso));
}

// The reader's timezone, or UTC where there is none to ask: on the server, and
// in the instant before a browser has said.
export function browserZone(): string {
  try {
    return Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC";
  } catch {
    return "UTC";
  }
}
