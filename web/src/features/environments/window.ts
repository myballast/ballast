import type { AllowedWindow, Day } from "./types";

// The days in week order, Monday first, as the API spells them.
export const weekdays: Day[] = ["mon", "tue", "wed", "thu", "fri", "sat", "sun"];

// describeDays says a set of days the way a person writes it: a run of
// consecutive days as a range, the rest separated by commas, and a range that
// goes round the end of the week — fri, sat, sun, mon — as one. It mirrors
// DayMask.String in internal/domain, so that the table and a refusal message
// say the same thing about the same window.
export function describeDays(days: Day[] | undefined): string {
  const on = weekdays.map((day) => days === undefined || days.length === 0 || days.includes(day));
  if (on.every(Boolean)) return "every day";

  // Start just after a gap, so that a run that wraps past Sunday is walked as one.
  const start = on.findIndex((isOn, i) => isOn && !on[(i + 6) % 7]);
  const parts: string[] = [];
  for (let walked = 0; walked < 7; ) {
    const i = (start + walked) % 7;
    if (!on[i]) {
      walked += 1;
      continue;
    }
    let length = 1;
    while (walked + length < 7 && on[(i + length) % 7]) length += 1;
    parts.push(length === 1 ? weekdays[i] : `${weekdays[i]}–${weekdays[(i + length - 1) % 7]}`);
    walked += length;
  }

  // Ranges in week order rather than in the order they were walked from the gap.
  return parts.sort((a, b) => weekdays.indexOf(a.slice(0, 3) as Day) - weekdays.indexOf(b.slice(0, 3) as Day)).join(", ");
}

// describeWindow is the table's "Allowed window": the days, the hours and the
// zone, or "any time" for an environment that takes load at every hour.
export function describeWindow(window: AllowedWindow | undefined): string {
  if (!window) return "any time";

  const hours = `${window.start}–${window.end}`;
  const everyDay = !window.days || window.days.length === 0 || window.days.length === 7;
  return everyDay ? `${hours} ${window.timezone}` : `${describeDays(window.days)} ${hours} ${window.timezone}`;
}
