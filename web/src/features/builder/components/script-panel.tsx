"use client";

import { Check, Copy } from "lucide-react";
import { useState } from "react";

import { Button } from "@/components/ui/button";

export type Draft =
  | { kind: "same" } // nothing changed since the last save
  | { kind: "unchecked" } // changed, and the server has not written it yet
  | { kind: "stale" } // changed again since it was written
  | { kind: "errors"; count: number } // the graph is wrong, so no script can be written
  | { kind: "ready"; script: string };

// ScriptPanel shows the k6 script, read only, with its lines numbered and a button
// to copy it.
//
// There are two to show. The saved one is what a run executes: it is stored when the
// version is, and the run reads it back as it is. The draft is what a save of the
// graph as it stands would store — written by the same function, so the two cannot
// differ for the same graph. It is a preview of a save and says so.
export function ScriptPanel({
  saved,
  version,
  draft,
  onWrite,
  pending,
  canWrite,
}: {
  saved: string;
  version: number;
  draft: Draft;
  // Asks the server to write the draft.
  onWrite: () => void;
  pending: boolean;
  canWrite: boolean;
}) {
  const [which, setWhich] = useState<"saved" | "draft">("saved");
  const [copied, setCopied] = useState(false);

  const showing = which === "draft" && draft.kind === "ready" ? draft.script : saved;
  const lines = showing.split("\n");
  if (lines.at(-1) === "") lines.pop();

  async function copy() {
    try {
      await navigator.clipboard.writeText(showing);
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    } catch {
      // No clipboard in this context (an insecure page, or refused): the text is
      // there to be selected, which is what the focusable block is for.
      setCopied(false);
    }
  }

  return (
    <section aria-labelledby="script-title" className="flex flex-col gap-3">
      <div className="flex flex-wrap items-center gap-2">
        <h2 id="script-title" className="sr-only">
          Generated script
        </h2>

        <div role="group" aria-label="Which script" className="flex rounded-lg border p-0.5 text-xs">
          <button
            type="button"
            aria-pressed={which === "saved"}
            onClick={() => setWhich("saved")}
            className="rounded-md px-2.5 py-1 aria-pressed:bg-muted aria-pressed:font-medium"
          >
            Saved <span className="data">v{version}</span>
          </button>
          <button
            type="button"
            aria-pressed={which === "draft"}
            onClick={() => setWhich("draft")}
            className="rounded-md px-2.5 py-1 aria-pressed:bg-muted aria-pressed:font-medium"
          >
            Current draft
          </button>
        </div>

        <Button type="button" variant="outline" size="sm" onClick={copy} className="ml-auto">
          {copied ? <Check aria-hidden /> : <Copy aria-hidden />}
          {copied ? "Copied" : "Copy"}
        </Button>
      </div>

      <p className="text-xs text-muted-foreground" role="status">
        {which === "saved" && `This is the script a run executes: version ${version}, as it was stored.`}
        {which === "draft" && draft.kind === "same" && "The draft is the saved version: nothing has changed since the last save."}
        {which === "draft" && draft.kind === "ready" && "What a save of the graph as it stands would store. It is not saved yet, and no run uses it."}
        {which === "draft" && draft.kind === "errors" &&
          `The graph has ${draft.count} ${draft.count === 1 ? "error" : "errors"}, so no script can be written for it yet. They are listed under Problems.`}
        {which === "draft" && (draft.kind === "unchecked" || draft.kind === "stale") &&
          (draft.kind === "stale" ? "The graph changed since this script was written." : "The draft has not been written yet.")}
        {" "}A secret is read from the environment when the run starts: only its name is in the script.
      </p>

      {which === "draft" && (draft.kind === "unchecked" || draft.kind === "stale") && canWrite && (
        <Button type="button" variant="outline" size="sm" className="w-fit" onClick={onWrite} disabled={pending}>
          {pending ? "Writing…" : "Write the draft"}
        </Button>
      )}

      {(which === "saved" || draft.kind === "ready" || draft.kind === "same") && (
        <div
          tabIndex={0}
          role="region"
          aria-label="Generated k6 script, read only"
          className="data flex max-h-[28rem] overflow-auto rounded-xl border bg-background text-xs leading-5 focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none"
        >
          <div aria-hidden className="sticky left-0 shrink-0 border-r bg-muted/30 px-2 py-2 text-right text-muted-foreground select-none">
            {lines.map((_, index) => (
              <div key={index}>{index + 1}</div>
            ))}
          </div>
          <pre className="m-0 min-w-0 flex-1 px-3 py-2 whitespace-pre">{lines.join("\n")}</pre>
        </div>
      )}
    </section>
  );
}
