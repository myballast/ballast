import { maxCatalogEndpoints, maxChosenEndpoints } from "./import-selection.ts";
import type { CollectionRequest, ImportFinding } from "./types.ts";

export const rootLabel = "No folder";

export type RequestGroup = {
  label: string;
  requests: CollectionRequest[];
};

// matchesSearch looks for every word of the search in the method, the path, the
// name and the folders, whatever the case.
function matchesSearch(request: CollectionRequest, search: string): boolean {
  const words = search.toLowerCase().split(/\s+/).filter((word) => word !== "");
  const text = `${request.method} ${request.path} ${request.name} ${request.folders.join(" ")}`.toLowerCase();

  return words.every((word) => text.includes(word));
}

// groupByFolder gathers the requests that match the search under their folder,
// in the order the collection runs them. Unlike the endpoints of a specification
// they are not sorted: a collection has a journey, and its order means something.
export function groupByFolder(requests: CollectionRequest[], search: string, noFolder = rootLabel): RequestGroup[] {
  const groups: RequestGroup[] = [];

  for (const request of requests) {
    if (!matchesSearch(request, search)) continue;

    const label = request.folders.length === 0 ? noFolder : request.folders.join(" / ");
    // Consecutive requests of the same folder make one group; the same folder
    // met again later is a group of its own, so that the order is not rewritten.
    const last = groups[groups.length - 1];

    if (last && last.label === label) {
      last.requests.push(request);
    } else {
      groups.push({ label, requests: [request] });
    }
  }
  return groups;
}

// firstChoice ticks the requests that can become part of a scenario, up to what
// one scenario holds, in the order of the collection.
export function firstChoice(requests: CollectionRequest[], limit = maxChosenEndpoints): Set<number> {
  const chosen = new Set<number>();

  for (const request of requests) {
    if (request.left_out) continue;
    if (chosen.size >= limit) break;
    chosen.add(request.position);
  }
  return chosen;
}

// chosenInOrder lists the chosen positions in the order of the collection, which
// is the order the requests will have in the scenario.
export function chosenPositions(requests: CollectionRequest[], chosen: ReadonlySet<number>): number[] {
  return requests.filter((request) => chosen.has(request.position)).map((request) => request.position);
}

// toggleGroup ticks every request of a group that can be chosen, or unticks them
// all when they were all ticked already.
export function toggleGroup(chosen: ReadonlySet<number>, group: RequestGroup): Set<number> {
  const next = new Set(chosen);
  const usable = group.requests.filter((request) => !request.left_out);
  const allChosen = usable.every((request) => next.has(request.position));

  for (const request of usable) {
    if (allChosen) next.delete(request.position);
    else next.add(request.position);
  }
  return next;
}

// limitFor is how many requests the chosen destination takes. A scenario starts
// with a node for each variable the requests need, and those leave less room.
export function limitFor(makeScenario: boolean, variableNodes = 0): number {
  return makeScenario ? Math.max(0, maxChosenEndpoints - variableNodes) : maxCatalogEndpoints;
}

export type FindingGroup = {
  // The request the findings are about; empty for the collection itself.
  request: string;
  messages: string[];
};

// groupFindings gathers the findings of an import by the request they are about,
// in the order they first appear, so that a report reads request by request.
export function groupFindings(findings: ImportFinding[]): FindingGroup[] {
  const groups: FindingGroup[] = [];

  for (const finding of findings) {
    const existing = groups.find((group) => group.request === finding.request);

    if (existing) {
      if (!existing.messages.includes(finding.message)) existing.messages.push(finding.message);
    } else {
      groups.push({ request: finding.request, messages: [finding.message] });
    }
  }
  return groups;
}
