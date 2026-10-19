// Number and time formatting shared by every screen.
//
// One locale for the whole interface, so the same number never reads two ways
// on two screens.
import { formatMoment } from "./time";

const locale = "en-GB";

const integer = new Intl.NumberFormat(locale, { maximumFractionDigits: 0 });
const oneDecimal = new Intl.NumberFormat(locale, {
  minimumFractionDigits: 1,
  maximumFractionDigits: 1,
});
const twoDecimals = new Intl.NumberFormat(locale, {
  minimumFractionDigits: 2,
  maximumFractionDigits: 2,
});

export const missing = "—";

// formatMs prints a latency: milliseconds below a second, seconds above.
export function formatMs(ms: number | null): string {
  if (ms === null) return missing;
  if (ms < 10) return `${twoDecimals.format(ms)} ms`;
  if (ms < 1000) return `${integer.format(ms)} ms`;
  return `${twoDecimals.format(ms / 1000)} s`;
}

export function formatRate(perSecond: number | null): string {
  if (perSecond === null) return missing;
  return `${oneDecimal.format(perSecond)} req/s`;
}

// formatPercent takes a ratio between 0 and 1, as k6 reports rates.
export function formatPercent(ratio: number | null): string {
  if (ratio === null) return missing;
  return `${twoDecimals.format(ratio * 100)} %`;
}

const clockFormats = new Map<string, Intl.DateTimeFormat>();
const dayFormats = new Map<string, Intl.DateTimeFormat>();

function inZone(cache: Map<string, Intl.DateTimeFormat>, zone: string | undefined, options: Intl.DateTimeFormatOptions) {
  const key = zone ?? "";
  let found = cache.get(key);
  if (!found) {
    found = new Intl.DateTimeFormat(locale, { ...options, timeZone: zone });
    cache.set(key, found);
  }
  return found;
}

// formatClock prints a moment as the time of day, "14:02:11", and puts the day
// in front of it when it is not today: a bare time of day on a run from last
// week would read as this morning's. now is passed in, like formatRelative's, and
// zone is the reader's, which decides what "today" is.
export function formatClock(iso: string | undefined, now: Date, zone?: string): string {
  if (!iso) return missing;

  const clock = inZone(clockFormats, zone, { hour: "2-digit", minute: "2-digit", second: "2-digit", hourCycle: "h23" });
  const day = inZone(dayFormats, zone, { day: "2-digit", month: "short", year: "numeric" });

  const at = new Date(iso);
  const sameDay = day.format(at) === day.format(now);
  const dayOnly = inZone(dayFormats, zone, { day: "2-digit", month: "short" });
  return sameDay ? clock.format(at) : `${dayOnly.format(at)}, ${clock.format(at)}`;
}

// formatDateTime is the one way a date and time are written. The components that
// show one use <Time>, which does it in the reader's zone; this is for text that
// is built outside a component.
export function formatDateTime(iso: string | undefined, zone?: string): string {
  if (!iso) return missing;
  return formatMoment(iso, zone);
}

// formatDuration prints the time between two instants as "8m 14s".
export function formatDuration(
  from: string | undefined,
  to: string | undefined,
): string {
  if (!from || !to) return missing;

  return formatSeconds((new Date(to).getTime() - new Date(from).getTime()) / 1000);
}

// formatSeconds prints a length of time given in seconds as "8m 14s".
export function formatSeconds(seconds: number | null): string {
  if (seconds === null) return missing;

  const total = Math.max(0, Math.round(seconds));
  const hours = Math.floor(total / 3600);
  const minutes = Math.floor((total % 3600) / 60);
  const rest = total % 60;

  if (hours > 0) return `${hours}h ${minutes}m ${rest}s`;
  if (minutes > 0) return `${minutes}m ${rest}s`;
  return `${rest}s`;
}

const relative = new Intl.RelativeTimeFormat(locale, { numeric: "auto" });

const units: [Intl.RelativeTimeFormatUnit, number][] = [
  ["year", 365 * 24 * 3600],
  ["month", 30 * 24 * 3600],
  ["day", 24 * 3600],
  ["hour", 3600],
  ["minute", 60],
];

// formatRelative prints how long ago something happened: "3 hours ago". now
// is passed in rather than read, so that the server and a test agree on it.
export function formatRelative(iso: string | undefined, now: Date): string {
  if (!iso) return missing;

  const seconds = Math.round((new Date(iso).getTime() - now.getTime()) / 1000);
  for (const [unit, size] of units) {
    if (Math.abs(seconds) >= size) {
      return relative.format(Math.trunc(seconds / size), unit);
    }
  }
  return "just now";
}

// formatBytes prints a size in bytes, kilobytes or megabytes, in powers of
// 1024, which is how k6 counts data_received.
export function formatBytes(bytes: number | null): string {
  if (bytes === null) return missing;
  if (bytes < 1024) return `${integer.format(bytes)} B`;
  if (bytes < 1024 * 1024) return `${oneDecimal.format(bytes / 1024)} KiB`;
  return `${oneDecimal.format(bytes / (1024 * 1024))} MiB`;
}

export function formatCount(value: number | null): string {
  return value === null ? missing : integer.format(value);
}
