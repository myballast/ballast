import type { CatalogEndpoint } from "../endpoints/types.ts";

// What typing in a request's path offers: the project's catalog endpoints that fit
// what has been typed, best fit first. Every word typed must be found somewhere in
// the endpoint — its method, path, summary or tag — so that "post login" finds
// POST /auth/login.
export function suggestEndpoints(endpoints: CatalogEndpoint[], typed: string, limit = 8): CatalogEndpoint[] {
  const words = typed.toLowerCase().split(/\s+/).filter(Boolean);

  const ranked: { endpoint: CatalogEndpoint; score: number; order: number }[] = [];
  endpoints.forEach((endpoint, order) => {
    const path = endpoint.path.toLowerCase();
    const haystack = `${endpoint.method} ${path} ${endpoint.summary} ${endpoint.tag}`.toLowerCase();
    if (!words.every((word) => haystack.includes(word))) return;

    // A path that starts with the first word is what somebody typing a path meant.
    const first = words[0] ?? "";
    const score = first === "" || path.startsWith(first) ? 0 : path.includes(first) ? 1 : 2;
    ranked.push({ endpoint, score, order });
  });

  return ranked
    .sort((a, b) => a.score - b.score || a.order - b.order)
    .slice(0, limit)
    .map((r) => r.endpoint);
}

// What to say beside a suggestion about what choosing it will not bring: a request
// with a body the file gave no example of (the catalog never guesses one from a
// schema), or an example too long for a node.
export function noteOf(endpoint: CatalogEndpoint): string | null {
  if (endpoint.body_too_long) return "example body too long to copy";
  if (endpoint.content_type !== "" && endpoint.body === "") return "no example body in the file";
  return null;
}
