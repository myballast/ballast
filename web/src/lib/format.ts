// Number and time formatting shared by every screen.
//
// One locale for the whole interface, so the same number never reads two ways
// on two screens.
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

const time = new Intl.DateTimeFormat(locale, {
  year: "numeric",
  month: "short",
  day: "2-digit",
  hour: "2-digit",
  minute: "2-digit",
  second: "2-digit",
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

export function formatDateTime(iso: string | undefined): string {
  if (!iso) return missing;
  return time.format(new Date(iso));
}

// formatDuration prints the time between two instants as "8m 14s".
export function formatDuration(
  from: string | undefined,
  to: string | undefined,
): string {
  if (!from || !to) return missing;

  const total = Math.max(
    0,
    Math.round((new Date(to).getTime() - new Date(from).getTime()) / 1000),
  );
  const hours = Math.floor(total / 3600);
  const minutes = Math.floor((total % 3600) / 60);
  const seconds = total % 60;

  if (hours > 0) return `${hours}h ${minutes}m ${seconds}s`;
  if (minutes > 0) return `${minutes}m ${seconds}s`;
  return `${seconds}s`;
}
