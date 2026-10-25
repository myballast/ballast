// Where the browser goes to sign in with provider.
//
// A path on this front end, not an address of the API: the browser only ever
// talks to one origin, so the cookie the sign-in sets lands on the host that
// reads it. Two hosts under a platform's shared domain cannot share a cookie, and
// a front end that waits for one the API gave to its own host waits for ever.
// The route handlers under app/auth relay the rest.
export function loginURL(provider: string, next: string): string {
  const params = new URLSearchParams({ next });
  return `/auth/${encodeURIComponent(provider)}/login?${params.toString()}`;
}

// Only a path on this front end; the API enforces the same rule.
export function safeNext(next: string | string[] | undefined): string {
  if (typeof next !== "string" || !next.startsWith("/") || next.startsWith("//") || next.includes("\\")) {
    return "/";
  }
  return next;
}
