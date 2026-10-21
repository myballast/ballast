"use client";

import { Search, Trash2 } from "lucide-react";
import { useState } from "react";

import { ConfirmAction } from "@/components/confirm-action";
import { groupByTag } from "@/features/scenarios/import-selection";

import { deleteCatalogEndpoint, deleteCatalogSource } from "../actions";
import type { CatalogEndpoint } from "../types";

const field =
  "h-9 rounded-md border bg-background px-3 text-sm outline-none focus-visible:ring-2 focus-visible:ring-ring";

// sourcesOf gathers the endpoints under the file they came from, in the order
// the files first appear.
function sourcesOf(endpoints: CatalogEndpoint[]): { name: string; endpoints: CatalogEndpoint[] }[] {
  const sources: { name: string; endpoints: CatalogEndpoint[] }[] = [];

  for (const endpoint of endpoints) {
    const existing = sources.find((source) => source.name === endpoint.source);
    if (existing) existing.endpoints.push(endpoint);
    else sources.push({ name: endpoint.source, endpoints: [endpoint] });
  }
  return sources;
}

// EndpointCatalog lists a project's endpoints by source and tag, with a search,
// and lets whoever may write scenarios remove one endpoint or a whole source.
export function EndpointCatalog({
  projectId,
  endpoints,
  canEdit,
}: {
  projectId: string;
  endpoints: CatalogEndpoint[];
  canEdit: boolean;
}) {
  const [search, setSearch] = useState("");

  const matching = groupByTag(endpoints, search).flatMap((group) => group.endpoints);
  const sources = sourcesOf(matching);

  return (
    <div className="flex flex-col gap-6">
      <label className="relative flex max-w-md items-center text-sm">
        <span className="sr-only">Search endpoints</span>
        <Search aria-hidden className="pointer-events-none absolute left-3 size-4 text-muted-foreground" />
        <input
          type="search"
          value={search}
          onChange={(event) => setSearch(event.target.value)}
          placeholder="Search by path, method, tag or summary"
          className={`${field} w-full pl-9`}
        />
      </label>

      {sources.length === 0 && (
        <p className="rounded-xl border border-dashed px-4 py-8 text-center text-sm text-muted-foreground">
          No endpoint matches.
        </p>
      )}

      {sources.map((source) => (
        <section key={source.name} className="flex flex-col rounded-xl border">
          <header className="flex flex-wrap items-center justify-between gap-3 border-b px-4 py-2.5">
            <h2 className="text-sm font-medium">
              {source.name}{" "}
              <span className="num text-xs font-normal text-muted-foreground">
                {source.endpoints.length} {source.endpoints.length === 1 ? "endpoint" : "endpoints"}
              </span>
            </h2>
            {canEdit && (
              <ConfirmAction
                icon={<Trash2 aria-hidden />}
                label="Remove source"
                variant="ghost"
                question={
                  <>
                    Remove every endpoint that <span className="font-medium">{source.name}</span> brought? Scenarios
                    already written from them are not changed.
                  </>
                }
                confirm="Remove the source"
                busy="Removing…"
                act={() => deleteCatalogSource(projectId, source.name)}
              />
            )}
          </header>

          <ul className="flex flex-col divide-y">
            {source.endpoints.map((endpoint) => (
              <li key={endpoint.id} className="flex flex-wrap items-start justify-between gap-3 px-4 py-2.5 text-sm">
                <span className="flex min-w-0 flex-col gap-0.5">
                  <span className="data flex gap-2 break-all">
                    <span className="w-14 shrink-0 text-muted-foreground">{endpoint.method}</span>
                    <span>{endpoint.path}</span>
                  </span>
                  <span className="flex flex-wrap gap-x-3 text-xs text-muted-foreground">
                    {endpoint.tag && <span>{endpoint.tag}</span>}
                    {endpoint.summary && <span>{endpoint.summary}</span>}
                    {endpoint.content_type && <span>has a body</span>}
                    {endpoint.body_too_long && <span>example too long to keep</span>}
                  </span>
                </span>
                {canEdit && (
                  <ConfirmAction
                    icon={<Trash2 aria-hidden />}
                    label="Remove"
                    variant="ghost"
                    question={
                      <>
                        Remove <span className="data">{endpoint.method} {endpoint.path}</span> from the endpoints?
                      </>
                    }
                    confirm="Remove"
                    busy="Removing…"
                    act={() => deleteCatalogEndpoint(projectId, endpoint.id)}
                  />
                )}
              </li>
            ))}
          </ul>
        </section>
      ))}
    </div>
  );
}
