import type { OpenAPIEndpoint } from "./types";

// The most requests one scenario can hold: 200 nodes, one of them the end.
// Mirrors maxImportedEndpoints in internal/domain/openapi_import.go.
export const maxChosenEndpoints = 199;

// The largest specification the API reads. Mirrors domain.MaxSpecBytes.
export const maxSpecBytes = 5 * 1024 * 1024;

export const untaggedLabel = "Untagged";

export type EndpointGroup = {
  label: string;
  endpoints: OpenAPIEndpoint[];
};

// endpointKey identifies an endpoint in a set of chosen ones.
export function endpointKey(endpoint: Pick<OpenAPIEndpoint, "method" | "path">): string {
  return `${endpoint.method} ${endpoint.path}`;
}

// matchesSearch looks for every word of the search in the method, the path, the
// summary and the tag, in any order and whatever the case.
function matchesSearch(endpoint: OpenAPIEndpoint, search: string): boolean {
  const words = search.toLowerCase().split(/\s+/).filter((word) => word !== "");
  const text = `${endpoint.method} ${endpoint.path} ${endpoint.summary} ${endpoint.tag}`.toLowerCase();

  return words.every((word) => text.includes(word));
}

// groupByTag keeps the endpoints that match the search and gathers them under
// their tag, in the order the tags first appear. The API already sends them
// tagged first, so the groups come out in the same order as the list.
export function groupByTag(endpoints: OpenAPIEndpoint[], search: string): EndpointGroup[] {
  const groups: EndpointGroup[] = [];

  for (const endpoint of endpoints) {
    if (!matchesSearch(endpoint, search)) continue;

    const label = endpoint.tag === "" ? untaggedLabel : endpoint.tag;
    const existing = groups.find((group) => group.label === label);

    if (existing) {
      existing.endpoints.push(endpoint);
    } else {
      groups.push({ label, endpoints: [endpoint] });
    }
  }

  return groups;
}

// chosenInOrder lists the chosen endpoints in the order of the whole list, not
// the order they were ticked in and not the order a search left them in. That
// is the order the requests will have in the scenario.
export function chosenInOrder(
  endpoints: OpenAPIEndpoint[],
  chosen: ReadonlySet<string>,
): { method: string; path: string }[] {
  const result: { method: string; path: string }[] = [];

  for (const endpoint of endpoints) {
    if (chosen.has(endpointKey(endpoint))) {
      result.push({ method: endpoint.method, path: endpoint.path });
    }
  }

  return result;
}

// toggleGroup ticks every endpoint of a group, or unticks them all when
// they were all ticked already.
export function toggleGroup(chosen: ReadonlySet<string>, group: EndpointGroup): Set<string> {
  const next = new Set(chosen);
  const allChosen = group.endpoints.every((endpoint) => next.has(endpointKey(endpoint)));

  for (const endpoint of group.endpoints) {
    if (allChosen) {
      next.delete(endpointKey(endpoint));
    } else {
      next.add(endpointKey(endpoint));
    }
  }

  return next;
}

// countWithPathParams counts the chosen requests that have a {name} in their path.
// They will need a value for it before they run.
export function countWithPathParams(endpoints: OpenAPIEndpoint[], chosen: ReadonlySet<string>): number {
  return endpoints.filter((endpoint) => chosen.has(endpointKey(endpoint)) && endpoint.path_params.length > 0).length;
}
