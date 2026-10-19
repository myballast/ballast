"use client";

import { LayoutGrid, List } from "lucide-react";
import { useOptimistic, useTransition } from "react";

import { cn } from "@/lib/utils";

import { savePreferences } from "../actions";
import { sortLabels, type SortKey, type ViewMode } from "../arrangement";

// ProjectToolbar is the order and the view of the list, remembered for this
// browser. The choice shows at once and is written behind it; the page reads it
// back from the cookie when it is drawn again, so a reload keeps it.
export function ProjectToolbar({ sort, view }: { sort: SortKey; view: ViewMode }) {
  const [choice, choose] = useOptimistic({ sort, view }, (current, next: Partial<{ sort: SortKey; view: ViewMode }>) => ({
    ...current,
    ...next,
  }));
  const [, startTransition] = useTransition();

  function change(next: { sort?: SortKey; view?: ViewMode }) {
    startTransition(async () => {
      choose(next);
      await savePreferences(next);
    });
  }

  return (
    <div className="flex items-center gap-2.5">
      <label className="flex items-center gap-2 text-xs text-muted-foreground">
        Sort
        <select
          value={choice.sort}
          onChange={(event) => change({ sort: event.target.value as SortKey })}
          className="h-8 rounded-md border bg-background px-2 text-xs text-foreground outline-none focus-visible:ring-2 focus-visible:ring-ring"
        >
          {(Object.keys(sortLabels) as SortKey[]).map((key) => (
            <option key={key} value={key}>
              {sortLabels[key]}
            </option>
          ))}
        </select>
      </label>

      <div role="group" aria-label="View" className="flex h-8 overflow-hidden rounded-md border">
        {(
          [
            { mode: "grid", label: "Grid view", icon: LayoutGrid },
            { mode: "list", label: "List view", icon: List },
          ] as const
        ).map(({ mode, label, icon: Icon }) => (
          <button
            key={mode}
            type="button"
            aria-pressed={choice.view === mode}
            aria-label={label}
            onClick={() => change({ view: mode })}
            className={cn(
              "grid w-8 place-items-center border-r last:border-r-0 text-muted-foreground hover:text-foreground",
              choice.view === mode && "bg-muted text-foreground",
            )}
          >
            <Icon aria-hidden className="size-3.5" />
          </button>
        ))}
      </div>
    </div>
  );
}
