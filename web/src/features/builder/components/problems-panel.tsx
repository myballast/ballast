"use client";

import { CircleX, TriangleAlert } from "lucide-react";

import { cn } from "@/lib/utils";

export type Problem = {
  // The node it is on; empty for the graph as a whole.
  nodeId: string;
  // What to call the node: its title on the canvas.
  where: string;
  message: string;
  level: "error" | "warning";
};

// ProblemsPanel lists what is wrong with the graph, each line on the node it is
// about. An error stops a save and a run; a warning is only worth knowing — a
// variable read before anything writes it may well be written by an earlier pass of
// a loop. Both say which they are in a word and an icon, and a click goes to the
// node.
export function ProblemsPanel({
  problems,
  checked,
  stale,
  onSelect,
}: {
  problems: Problem[];
  // Whether the server has looked the graph over, and the answer is about this graph.
  checked: boolean;
  // The graph changed since the last time it was looked over.
  stale: boolean;
  onSelect: (nodeId: string) => void;
}) {
  const errors = problems.filter((p) => p.level === "error").length;
  const warnings = problems.length - errors;

  return (
    <section aria-labelledby="problems-title" className="flex flex-col gap-2 rounded-xl border bg-card p-3">
      <div className="flex flex-wrap items-baseline gap-x-3 gap-y-1">
        <h2 id="problems-title" className="text-[11px] font-normal tracking-wider text-muted-foreground uppercase">
          Problems
        </h2>
        <p role="status" className="data text-xs text-muted-foreground">
          {errors} {errors === 1 ? "error" : "errors"} · {warnings} {warnings === 1 ? "warning" : "warnings"}
          {stale && " · changed since the last check: press Validate"}
          {!checked && !stale && " · not checked by the server yet"}
        </p>
      </div>

      {problems.length === 0 ? (
        <p className="text-xs text-muted-foreground">
          {checked && !stale ? "Nothing is wrong with this graph." : "Press Validate to look the whole graph over."}
        </p>
      ) : (
        <ul className={cn("flex max-h-48 flex-col divide-y overflow-auto rounded-lg border", stale && "opacity-70")}>
          {problems.map((problem, index) => (
            <li key={`${problem.nodeId}:${index}`}>
              <button
                type="button"
                disabled={problem.nodeId === ""}
                onClick={() => onSelect(problem.nodeId)}
                className="flex w-full items-start gap-2 px-2.5 py-1.5 text-left text-xs hover:bg-muted/60 focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none disabled:cursor-default disabled:hover:bg-transparent"
              >
                {problem.level === "error" ? (
                  <CircleX aria-hidden className="mt-0.5 size-3.5 shrink-0" />
                ) : (
                  <TriangleAlert aria-hidden className="mt-0.5 size-3.5 shrink-0" />
                )}
                <span className="data w-14 shrink-0">{problem.level === "error" ? "Error" : "Warning"}</span>
                <span className="data w-32 shrink-0 truncate text-muted-foreground" title={problem.where}>
                  {problem.where}
                </span>
                <span className="min-w-0 flex-1">{problem.message}</span>
              </button>
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}
