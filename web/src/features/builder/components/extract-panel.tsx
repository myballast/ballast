"use client";

import { CircleAlert } from "lucide-react";

import type { GraphNode } from "@/features/scenarios/types";

import { extractProblem } from "../request-validation";

const input =
  "data h-8 w-full rounded-md border bg-background px-2 text-xs outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:opacity-60";

const sources = [
  { id: "body", label: "Response body (JSON)" },
  { id: "header", label: "Response header" },
  { id: "status", label: "Status code" },
] as const;

type Source = (typeof sources)[number]["id"];

const hints: Record<Source, string> = {
  body: "A path in the JSON: data.session.token, or items.0.id for the first of a list. A leading $. is fine.",
  header: "The name of the header, whatever its case: X-Request-Id.",
  status: "The code the target answered, such as 200, as a number a condition can compare.",
};

// ExtractPanel is what an "Extract variable" node holds: where in the last response
// to read, what to read there, and the name the value is kept under. When the value
// is not there the generated script stops the iteration and says which variable it
// was — nothing is sent with an empty one — and the panel says so, since that is
// the part nobody would guess.
export function ExtractPanel({
  node,
  readOnly,
  onChange,
}: {
  node: GraphNode;
  readOnly: boolean;
  onChange: (patch: Partial<GraphNode>, field: string) => void;
}) {
  const source: Source = node.source ?? "body";
  const problem = extractProblem(node);

  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-col gap-1">
        <label htmlFor="extract-source" className="text-[11px] tracking-wider text-muted-foreground uppercase">
          Read from
        </label>
        <select
          id="extract-source"
          className={input}
          value={source}
          disabled={readOnly}
          // A status has no path: leaving the old one behind would be refused as a
          // value the new source does not take.
          onChange={(event) => {
            const next = event.target.value as Source;
            onChange({ source: next, from: next === "status" ? "" : (node.from ?? "") }, "source");
          }}
        >
          {sources.map((option) => (
            <option key={option.id} value={option.id}>
              {option.label}
            </option>
          ))}
        </select>
      </div>

      {source !== "status" && (
        <div className="flex flex-col gap-1">
          <label htmlFor="extract-from" className="text-[11px] tracking-wider text-muted-foreground uppercase">
            {source === "body" ? "Path" : "Header name"}
          </label>
          <input
            id="extract-from"
            className={input}
            value={node.from ?? ""}
            disabled={readOnly}
            spellCheck={false}
            aria-invalid={problem && !problem.startsWith("The variable") ? true : undefined}
            aria-describedby="extract-hint"
            onChange={(event) => onChange({ from: event.target.value }, "from")}
          />
        </div>
      )}

      <div className="flex flex-col gap-1">
        <label htmlFor="extract-variable" className="text-[11px] tracking-wider text-muted-foreground uppercase">
          Target variable
        </label>
        <input
          id="extract-variable"
          className={input}
          value={node.variable ?? ""}
          disabled={readOnly}
          spellCheck={false}
          aria-invalid={problem?.startsWith("The variable") ? true : undefined}
          aria-describedby="extract-hint"
          onChange={(event) => onChange({ variable: event.target.value }, "variable")}
        />
        <p id="extract-hint" className="text-xs text-muted-foreground">
          {hints[source]}
        </p>
      </div>

      {problem && (
        <p role="alert" className="flex items-start gap-1 text-xs text-muted-foreground">
          <CircleAlert aria-hidden className="mt-0.5 size-3 shrink-0" />
          {problem}
        </p>
      )}

      <p className="text-xs text-muted-foreground">
        If the value is missing or empty, the iteration stops and the run says which variable could not be read. The next
        request is not sent without it.
      </p>
    </div>
  );
}
