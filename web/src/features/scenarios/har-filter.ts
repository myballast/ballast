import type { CollectionRequest, RequestKind } from "./types.ts";

// The kinds a HAR request is sorted into, in the order the filter lists them.
export const requestKinds: { id: RequestKind; label: string }[] = [
  { id: "api", label: "API calls (XHR, fetch)" },
  { id: "document", label: "Pages" },
  { id: "script", label: "Scripts" },
  { id: "style", label: "Styles" },
  { id: "image", label: "Images" },
  { id: "font", label: "Fonts" },
  { id: "media", label: "Audio and video" },
  { id: "other", label: "Other" },
];

export type HostCount = { host: string; total: number; api: number };

// hostCounts lists the hosts a recording talked to, the busiest API first: the
// one a page calls for its data is the one to test, not the one that serves its
// images.
export function hostCounts(requests: CollectionRequest[]): HostCount[] {
  const counts = new Map<string, HostCount>();

  for (const request of requests) {
    const entry = counts.get(request.host) ?? { host: request.host, total: 0, api: 0 };
    entry.total += 1;
    if (request.kind === "api") entry.api += 1;
    counts.set(request.host, entry);
  }

  return [...counts.values()].sort((a, b) => b.api - a.api || b.total - a.total || a.host.localeCompare(b.host));
}

// defaultHost is the host to start from.
export function defaultHost(requests: CollectionRequest[]): string {
  return hostCounts(requests)[0]?.host ?? "";
}

// kindCounts says how many requests of a host there are of each kind.
export function kindCounts(requests: CollectionRequest[], host: string): Map<RequestKind, number> {
  const counts = new Map<RequestKind, number>();

  for (const request of requests) {
    if (request.host !== host || request.kind === "") continue;
    counts.set(request.kind, (counts.get(request.kind) ?? 0) + 1);
  }
  return counts;
}

// defaultKinds ticks the API calls. A recording with none, such as one from a
// browser that does not say what a request was for, starts with every kind
// ticked instead of an empty list.
export function defaultKinds(requests: CollectionRequest[], host: string): Set<RequestKind> {
  const counts = kindCounts(requests, host);
  if ((counts.get("api") ?? 0) > 0) return new Set<RequestKind>(["api"]);
  return new Set(counts.keys());
}

// visibleRequests keeps the requests of one host and of the ticked kinds, in the
// order recorded.
export function visibleRequests(
  requests: CollectionRequest[],
  host: string,
  kinds: ReadonlySet<RequestKind>,
): CollectionRequest[] {
  return requests.filter((request) => request.host === host && request.kind !== "" && kinds.has(request.kind));
}
