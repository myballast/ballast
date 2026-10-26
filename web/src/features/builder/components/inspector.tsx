"use client";

import { Trash2, X } from "lucide-react";
import { useId, useState } from "react";

import { Button } from "@/components/ui/button";
import type { GraphNode } from "@/features/scenarios/types";

import { describe, infoOf } from "../kinds";
import type { Available } from "../variables";
import { AvailableVariables } from "./available-variables";
import { ExtractPanel } from "./extract-panel";
import { KindIcon } from "./kind-icon";
import { RequestPanel } from "./request-panel";

const input =
  "data h-8 w-full rounded-md border bg-background px-2 text-xs outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:opacity-60";

function Field({ label, hint, children }: { label: string; hint?: string; children: (id: string) => React.ReactNode }) {
  const id = useId();
  return (
    <div className="flex flex-col gap-1">
      <label htmlFor={id} className="text-[11px] tracking-wider text-muted-foreground uppercase">
        {label}
      </label>
      {children(id)}
      {hint && <p className="text-xs text-muted-foreground">{hint}</p>}
    </div>
  );
}

// Inspector is the panel to the right of the canvas: what the selected node holds,
// as fields. For somebody who cannot write scenarios every field is disabled, so
// they can read it all and change nothing.
export function Inspector({
  node,
  readOnly,
  revision,
  onChange,
  onDelete,
  onClose,
  request,
  available,
}: {
  node: GraphNode;
  // What the steps before this one write. Shown under every step that has anything
  // to read.
  available: Available[];
  readOnly: boolean;
  // What a request node needs besides itself: the catalog, the variables it can
  // read, and the way to bind a secret.
  request: Omit<React.ComponentProps<typeof RequestPanel>, "node" | "readOnly" | "onChange">;
  // Changes when an undo rewrites the node, so that the rows start again from it.
  revision: number;
  onChange: (patch: Partial<GraphNode>, field: string) => void;
  onDelete: () => void;
  onClose: () => void;
}) {
  const info = infoOf(node.kind);
  const { title } = describe(node);
  const [mode, setMode] = useState<"value" | "secret">(node.from_env ? "secret" : "value");

  return (
    <aside aria-label="Node inspector" className="flex h-full flex-col overflow-hidden rounded-xl border bg-card">
      <div className="flex items-center gap-2 border-b px-3 py-2.5">
        <KindIcon icon={info.icon} className="size-4 text-muted-foreground" />
        <div className="min-w-0 flex-1">
          <div className="truncate text-sm">{info.label}</div>
          <div className="data truncate text-[11px] text-muted-foreground" title={title}>
            {node.id}
          </div>
        </div>
        <Button type="button" variant="ghost" size="icon-sm" aria-label="Close inspector" onClick={onClose}>
          <X aria-hidden />
        </Button>
      </div>

      <div key={`${node.id}:${revision}`} className="flex min-h-0 flex-1 flex-col gap-4 overflow-y-auto p-3">
        <fieldset disabled={readOnly} className="flex min-w-0 flex-col gap-4">
          <Field label="Label" hint="A name for people. It is written in the script as a comment.">
            {(id) => (
              <input id={id} className={input} value={node.label ?? ""} onChange={(e) => onChange({ label: e.target.value }, "label")} />
            )}
          </Field>

          {node.kind === "http_request" && <RequestPanel node={node} readOnly={readOnly} onChange={onChange} {...request} />}

          {node.kind === "extract_variable" && <ExtractPanel node={node} readOnly={readOnly} onChange={onChange} />}

          {node.kind === "set_variable" && (
            <>
              <Field label="Variable">
                {(id) => <input id={id} className={input} value={node.variable ?? ""} onChange={(e) => onChange({ variable: e.target.value }, "variable")} />}
              </Field>
              <Field label="Taken from">
                {(id) => (
                  <select
                    id={id}
                    className={input}
                    value={mode}
                    onChange={(e) => {
                      const next = e.target.value as "value" | "secret";
                      setMode(next);
                      // One or the other, never both: the API refuses a node with both.
                      onChange(next === "secret" ? { value: "", from_env: node.from_env ?? "" } : { from_env: "" }, "mode");
                    }}
                  >
                    <option value="value">a value</option>
                    <option value="secret">a secret of the environment</option>
                  </select>
                )}
              </Field>
              {mode === "secret" ? (
                <Field label="Secret key" hint="Read when the run starts. The value is never written in the script.">
                  {(id) => <input id={id} className={input} value={node.from_env ?? ""} onChange={(e) => onChange({ from_env: e.target.value }, "from_env")} />}
                </Field>
              ) : (
                <Field label="Value">
                  {(id) => <input id={id} className={input} value={node.value ?? ""} onChange={(e) => onChange({ value: e.target.value }, "value")} />}
                </Field>
              )}
            </>
          )}

          {(node.kind === "condition" || node.kind === "assertion") && (
            <Field label="Expression" hint='Comparisons joined by && or ||: status == 200 && name == "alice".'>
              {(id) => <input id={id} className={input} value={node.expression ?? ""} onChange={(e) => onChange({ expression: e.target.value }, "expression")} />}
            </Field>
          )}

          {node.kind === "loop" && (
            <Field label="Repetitions" hint="The body repeats this many times, then the other way out is taken.">
              {(id) => (
                <input
                  id={id}
                  type="number"
                  min={1}
                  className={input}
                  value={node.iterations ?? ""}
                  onChange={(e) => onChange({ iterations: Number.parseInt(e.target.value, 10) || 0 }, "iterations")}
                />
              )}
            </Field>
          )}

          {node.kind === "think_time" && (
            <Field label="Seconds" hint="Between 0 and 300.">
              {(id) => (
                <input
                  id={id}
                  type="number"
                  min={0}
                  step="0.1"
                  className={input}
                  value={node.seconds ?? ""}
                  onChange={(e) => onChange({ seconds: Number.parseFloat(e.target.value) || 0 }, "seconds")}
                />
              )}
            </Field>
          )}

          {node.kind === "end" && <p className="text-xs text-muted-foreground">A path of the scenario stops here.</p>}
        </fieldset>

        {node.kind !== "end" && <AvailableVariables available={available} />}
      </div>

      {!readOnly && (
        <div className="border-t p-3">
          <Button type="button" variant="outline" size="sm" onClick={onDelete}>
            <Trash2 aria-hidden />
            Delete node
          </Button>
        </div>
      )}
    </aside>
  );
}
