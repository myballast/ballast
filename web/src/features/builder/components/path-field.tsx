"use client";

import { useId, useState } from "react";

import type { CatalogEndpoint } from "@/features/endpoints/types";
import { cn } from "@/lib/utils";

import { noteOf, suggestEndpoints } from "../endpoint-suggest";

// PathField is the path of a request, with the project's catalog offered as it is
// typed: choosing an endpoint fills the method, the path, the headers and the body
// at once, where the list is a help and not a rule — what is typed stays the path.
//
// A combobox: the arrows move in the list, Enter chooses, Escape closes it.
export function PathField({
  id,
  value,
  endpoints,
  invalid,
  describedBy,
  onChange,
  onPick,
}: {
  id: string;
  value: string;
  // The catalog; none or empty means a plain field.
  endpoints?: CatalogEndpoint[];
  invalid?: boolean;
  describedBy?: string;
  onChange: (path: string) => void;
  onPick: (endpoint: CatalogEndpoint) => void;
}) {
  const list = useId();
  const [focused, setFocused] = useState(false);
  const [index, setIndex] = useState(0);
  // Where the list was closed with Escape: it stays closed until the text changes.
  const [closedFor, setClosedFor] = useState<string | null>(null);

  const offered = endpoints && endpoints.length > 0 ? suggestEndpoints(endpoints, value) : [];
  const open = focused && offered.length > 0 && closedFor !== value;
  const active = Math.min(index, Math.max(0, offered.length - 1));

  function choose(endpoint: CatalogEndpoint) {
    // The path becomes the endpoint's: the list stays closed on it.
    setClosedFor(endpoint.path);
    onPick(endpoint);
  }

  return (
    <div className="relative flex min-w-0 flex-1">
      <input
        id={id}
        role={endpoints && endpoints.length > 0 ? "combobox" : undefined}
        aria-expanded={endpoints && endpoints.length > 0 ? open : undefined}
        aria-controls={open ? list : undefined}
        aria-activedescendant={open ? `${list}-${active}` : undefined}
        aria-autocomplete={endpoints && endpoints.length > 0 ? "list" : undefined}
        aria-invalid={invalid ? true : undefined}
        aria-describedby={describedBy}
        className="data h-8 min-w-0 flex-1 bg-transparent px-0.5 text-xs outline-none"
        value={value}
        spellCheck={false}
        autoComplete="off"
        onFocus={() => setFocused(true)}
        onBlur={() => setFocused(false)}
        onChange={(event) => {
          setIndex(0);
          onChange(event.target.value);
        }}
        onKeyDown={(event) => {
          if (!open) return;
          if (event.key === "ArrowDown") {
            event.preventDefault();
            setIndex((active + 1) % offered.length);
          } else if (event.key === "ArrowUp") {
            event.preventDefault();
            setIndex((active - 1 + offered.length) % offered.length);
          } else if (event.key === "Enter") {
            event.preventDefault();
            choose(offered[active]);
          } else if (event.key === "Escape") {
            event.preventDefault();
            setClosedFor(value);
          }
        }}
      />

      {open && (
        <ul
          id={list}
          role="listbox"
          aria-label="Endpoints of the catalog"
          className="absolute top-full right-0 left-[-5.5rem] z-20 mt-1.5 max-h-64 overflow-y-auto rounded-md border bg-popover p-1"
        >
          {offered.map((endpoint, i) => {
            const note = noteOf(endpoint);
            return (
              // The list is driven from the field; a mouse press must not take the
              // focus from it, or the list would close before the click lands.
              <li
                key={endpoint.id}
                id={`${list}-${i}`}
                role="option"
                aria-selected={i === active}
                onMouseDown={(event) => event.preventDefault()}
                onMouseEnter={() => setIndex(i)}
                onClick={() => choose(endpoint)}
                className={cn("flex cursor-pointer flex-col gap-0.5 rounded-sm px-2 py-1.5 text-xs", i === active && "bg-muted")}
              >
                <span className="flex items-baseline gap-2">
                  <span className="data w-12 shrink-0 text-muted-foreground">{endpoint.method}</span>
                  <span className="data min-w-0 flex-1 truncate">{endpoint.path}</span>
                </span>
                {(endpoint.summary || note) && (
                  <span className="truncate pl-14 text-[11px] text-muted-foreground">
                    {[endpoint.summary, note].filter(Boolean).join(" · ")}
                  </span>
                )}
              </li>
            );
          })}
          <li aria-hidden className="px-2 pt-1 pb-0.5 text-[11px] text-muted-foreground">
            Fills the method, path, headers and body. Undo restores them.
          </li>
        </ul>
      )}
    </div>
  );
}
