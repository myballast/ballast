import { templatePath } from "../endpoints/snippet.ts";
import type { CatalogEndpoint } from "../endpoints/types.ts";
import type { GraphNode } from "../scenarios/types.ts";

// What choosing an endpoint of the catalog fills in a request node: method, path,
// headers and body. The path keeps its template — /users/{id} — because that text
// is the request's name tag, and a name between braces that cannot be a variable
// is rewritten the way the API does when it builds a graph.
//
// Only those four things. The node's id, its label and whether it is critical are
// the node's own and stay as they were.
export function fromEndpoint(endpoint: CatalogEndpoint): Pick<GraphNode, "method" | "path" | "headers" | "body" | "label"> {
  const headers = { ...endpoint.headers };

  // A body travels with its type, or a server may not read it. Never overwritten:
  // a header the specification gave wins.
  const hasType = Object.keys(headers).some((name) => name.toLowerCase() === "content-type");
  if (endpoint.body !== "" && endpoint.content_type !== "" && !hasType) headers["Content-Type"] = endpoint.content_type;

  return {
    method: endpoint.method,
    path: templatePath(endpoint.path),
    headers: Object.keys(headers).length > 0 ? headers : undefined,
    body: endpoint.body,
    label: endpoint.summary.trim() === "" ? `${endpoint.method} ${endpoint.path}` : endpoint.summary.trim().slice(0, 200),
  };
}
