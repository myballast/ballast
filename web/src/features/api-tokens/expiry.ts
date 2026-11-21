// How long a new token lives. The API applies 90 days when none is given and
// refuses anything past a year from its own clock; these are the choices the
// form offers, said in words.
export const expiryOptions = [
  { days: 30, label: "30 days" },
  { days: 90, label: "90 days" },
  { days: 180, label: "180 days" },
  // A day short of a year on purpose: the API measures the ceiling from its own
  // clock, and this server's may be a little ahead of it. "1 year" must never
  // turn into a refusal because of that.
  { days: 364, label: "1 year" },
] as const;

export const defaultExpiryDays = 90;

// parseExpiryDays reads the choice from the form, and is null for anything that
// is not one of the listed numbers: the form posts a string, and a string is
// somebody else's input.
export function parseExpiryDays(raw: string): number | null {
  const option = expiryOptions.find((candidate) => String(candidate.days) === raw);
  return option ? option.days : null;
}

// expiresAtFor is the moment, in UTC, that many whole days from now.
export function expiresAtFor(days: number, now: Date): string {
  return new Date(now.getTime() + days * 24 * 60 * 60 * 1000).toISOString();
}
