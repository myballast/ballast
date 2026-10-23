"use client";

import { Trash2, X } from "lucide-react";
import { useId, useState } from "react";

import { Button } from "@/components/ui/button";
import type { GraphNode } from "@/features/scenarios/types";

import { describe, infoOf } from "../kinds";
import { KindIcon } from "./kind-icon";

const methods = ["GET", "POST", "PUT", "PATCH", "DELETE", "HEAD"];

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

type Row = { k: string; v: string };

// Rows of name and value: a request's headers and query. The rows are kept here,
// blanks included, so that a row being typed does not vanish; only the complete
// ones are handed up.
function Rows({
  label,
  addLabel,
  rows: initial,
  disabled,
  onChange,
}: {
  label: string;
  addLabel: string;
  rows: Row[];
  disabled: boolean;
  onChange: (rows: Row[]) => void;
}) {
  const [rows, setRows] = useState(initial);

  function change(next: Row[]) {
    setRows(next);
    onChange(next.filter((row) => row.k.trim() !== ""));
  }

  return (
    <fieldset className="flex flex-col gap-1.5" disabled={disabled}>
      <legend className="mb-1 text-[11px] tracking-wider text-muted-foreground uppercase">{label}</legend>
      {rows.map((row, index) => (
        <div key={index} className="flex gap-1.5">
          <input
            aria-label={`${label} name ${index + 1}`}
            value={row.k}
            onChange={(e) => change(rows.map((r, i) => (i === index ? { ...r, k: e.target.value } : r)))}
            className={input}
          />
          <input
            aria-label={`${label} value ${index + 1}`}
            value={row.v}
            onChange={(e) => change(rows.map((r, i) => (i === index ? { ...r, v: e.target.value } : r)))}
            className={input}
          />
          <Button
            type="button"
            variant="ghost"
            size="icon-sm"
            aria-label={`Remove ${label.toLowerCase()} ${index + 1}`}
            onClick={() => change(rows.filter((_, i) => i !== index))}
          >
            <X aria-hidden />
          </Button>
        </div>
      ))}
      {!disabled && (
        <Button type="button" variant="outline" size="sm" className="w-fit" aria-label={addLabel} onClick={() => change([...rows, { k: "", v: "" }])}>
          + Add
        </Button>
      )}
    </fieldset>
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
}: {
  node: GraphNode;
  readOnly: boolean;
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
        <fieldset disabled={readOnly} className="flex flex-col gap-4">
          <Field label="Label" hint="A name for people. It is written in the script as a comment.">
            {(id) => (
              <input id={id} className={input} value={node.label ?? ""} onChange={(e) => onChange({ label: e.target.value }, "label")} />
            )}
          </Field>

          {node.kind === "http_request" && (
            <>
              <div className="flex gap-2">
                <Field label="Method">
                  {(id) => (
                    <select id={id} className={input} value={node.method ?? "GET"} onChange={(e) => onChange({ method: e.target.value }, "method")}>
                      {methods.map((m) => (
                        <option key={m}>{m}</option>
                      ))}
                    </select>
                  )}
                </Field>
                <div className="min-w-0 flex-1">
                  <Field label="Path" hint="/users/{id}: a name between braces is a variable. It is also the request's name tag.">
                    {(id) => <input id={id} className={input} value={node.path ?? ""} onChange={(e) => onChange({ path: e.target.value }, "path")} />}
                  </Field>
                </div>
              </div>
              <Rows
                label="Headers"
                addLabel="Add a header"
                disabled={readOnly}
                rows={Object.entries(node.headers ?? {}).map(([k, v]) => ({ k, v }))}
                onChange={(rows) => onChange({ headers: Object.fromEntries(rows.map((r) => [r.k, r.v])) }, "headers")}
              />
              <Rows
                label="Query"
                addLabel="Add a query parameter"
                disabled={readOnly}
                rows={(node.query ?? []).map((q) => ({ k: q.name, v: q.value }))}
                onChange={(rows) => onChange({ query: rows.map((r) => ({ name: r.k, value: r.v })) }, "query")}
              />
              <Field label="Body" hint="{{name}} is a variable. A single brace is text.">
                {(id) => (
                  <textarea
                    id={id}
                    rows={5}
                    spellCheck={false}
                    className={`${input} h-auto py-1.5`}
                    value={node.body ?? ""}
                    onChange={(e) => onChange({ body: e.target.value }, "body")}
                  />
                )}
              </Field>
              <label className="flex items-center gap-2 text-xs">
                <input type="checkbox" checked={node.critical ?? false} onChange={(e) => onChange({ critical: e.target.checked }, "critical")} />
                Critical: the scenario stops here if this request fails
              </label>
            </>
          )}

          {node.kind === "extract_variable" && (
            <>
              <Field label="Target variable">
                {(id) => <input id={id} className={input} value={node.variable ?? ""} onChange={(e) => onChange({ variable: e.target.value }, "variable")} />}
              </Field>
              <Field label="JSONPath" hint="Read from the response of the previous request.">
                {(id) => <input id={id} className={input} value={node.from ?? ""} onChange={(e) => onChange({ from: e.target.value }, "from")} />}
              </Field>
            </>
          )}

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
