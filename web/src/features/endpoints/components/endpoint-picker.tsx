"use client";

import { Search, X } from "lucide-react";
import { useEffect, useRef, useState } from "react";

import { Button } from "@/components/ui/button";
import { groupByTag } from "@/features/scenarios/import-selection";

import type { CatalogEndpoint } from "../types";

const field =
  "h-9 rounded-md border bg-background px-3 text-sm outline-none focus-visible:ring-2 focus-visible:ring-ring";

// EndpointPicker lists the endpoints of a project's catalog, with a search, and
// tells whoever opened it which one was chosen. It decides nothing about what
// the choice is used for: the script editor writes a request from it, and the
// scenario canvas will fill a node from it.
export function EndpointPicker({
  endpoints,
  onPick,
  onClose,
}: {
  endpoints: CatalogEndpoint[];
  onPick: (endpoint: CatalogEndpoint) => void;
  onClose: () => void;
}) {
  const [search, setSearch] = useState("");
  const input = useRef<HTMLInputElement>(null);

  // Opened from a key or a button, and meant to be typed into straight away.
  useEffect(() => {
    input.current?.focus();
  }, []);

  const groups = groupByTag(endpoints, search);

  return (
    <div
      role="dialog"
      aria-label="Insert an endpoint"
      className="flex flex-col gap-3 rounded-lg border bg-card p-3"
      onKeyDown={(event) => {
        if (event.key === "Escape") onClose();
      }}
    >
      <div className="flex items-center gap-2">
        <label className="relative flex flex-1 items-center text-sm">
          <span className="sr-only">Search the endpoints</span>
          <Search aria-hidden className="pointer-events-none absolute left-3 size-4 text-muted-foreground" />
          <input
            ref={input}
            type="search"
            value={search}
            onChange={(event) => setSearch(event.target.value)}
            placeholder="Search by path, method, tag or summary"
            className={`${field} w-full pl-9`}
          />
        </label>
        <Button type="button" variant="ghost" size="icon" onClick={onClose} aria-label="Close">
          <X aria-hidden />
        </Button>
      </div>

      {groups.length === 0 ? (
        <p className="py-4 text-center text-sm text-muted-foreground">No endpoint matches.</p>
      ) : (
        <div className="flex max-h-72 flex-col gap-3 overflow-auto">
          {groups.map((group) => (
            <section key={group.label} className="flex flex-col gap-1">
              <h3 className="text-xs text-muted-foreground">{group.label}</h3>
              <ul className="flex flex-col">
                {group.endpoints.map((endpoint) => (
                  <li key={endpoint.id}>
                    <button
                      type="button"
                      onClick={() => onPick(endpoint)}
                      className="flex w-full flex-col gap-0.5 rounded-md px-2 py-1.5 text-left text-sm hover:bg-muted/60 focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none"
                    >
                      <span className="data flex gap-2 break-all">
                        <span className="w-14 shrink-0 text-muted-foreground">{endpoint.method}</span>
                        <span>{endpoint.path}</span>
                      </span>
                      {endpoint.summary && <span className="text-xs text-muted-foreground">{endpoint.summary}</span>}
                    </button>
                  </li>
                ))}
              </ul>
            </section>
          ))}
        </div>
      )}
    </div>
  );
}
