"use client";

import { createContext, useContext, useState } from "react";

import { cn } from "@/lib/utils";

export type RunTab = "summary" | "output";

// The tab on screen, for what has to know whether it is: the output, which
// follows the newest line only while it has a height to scroll.
const Current = createContext<RunTab>("summary");

export function useRunTab(): RunTab {
  return useContext(Current);
}

const tabs: { id: RunTab; label: string }[] = [
  { id: "summary", label: "Summary" },
  { id: "output", label: "Output" },
];

// RunTabs puts the two things a run has to show — what it measured, and what it
// said while it ran — side by side, one at a time.
//
// Both panels stay mounted and the inactive one is hidden, instead of being
// left out. The output follows the run over a connection that also tells the
// page when the run has started or ended, so that its status and its figures
// can be drawn again; closing it whenever somebody reads the summary would
// leave the summary stale. The tab is in the address (?tab=output), so a link
// can open on it, and changing it does not add a step to the browser's history.
export function RunTabs({
  initial,
  summary,
  output,
}: {
  initial: RunTab;
  summary: React.ReactNode;
  output: React.ReactNode;
}) {
  const [current, setCurrent] = useState<RunTab>(initial);

  const choose = (tab: RunTab) => {
    setCurrent(tab);
    const url = new URL(window.location.href);
    if (tab === "summary") url.searchParams.delete("tab");
    else url.searchParams.set("tab", tab);
    window.history.replaceState(null, "", url);
  };

  return (
    <Current.Provider value={current}>
      <div className="flex flex-col gap-6">
        <div role="tablist" aria-label="Run" className="flex gap-6 border-b">
          {tabs.map((tab) => (
            <button
              key={tab.id}
              type="button"
              role="tab"
              id={`run-tab-${tab.id}`}
              aria-selected={current === tab.id}
              aria-controls={`run-panel-${tab.id}`}
              onClick={() => choose(tab.id)}
              className={cn(
                "-mb-px border-b-[1.5px] border-transparent pb-2.5 text-[12.5px] text-muted-foreground hover:text-foreground",
                current === tab.id && "border-brand text-foreground",
              )}
            >
              {tab.label}
            </button>
          ))}
        </div>

        <div
          role="tabpanel"
          id="run-panel-summary"
          aria-labelledby="run-tab-summary"
          hidden={current !== "summary"}
          className="flex flex-col gap-6"
        >
          {summary}
        </div>
        <div
          role="tabpanel"
          id="run-panel-output"
          aria-labelledby="run-tab-output"
          hidden={current !== "output"}
        >
          {output}
        </div>
      </div>
    </Current.Provider>
  );
}
