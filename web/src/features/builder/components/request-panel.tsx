"use client";

import { CircleAlert, ListPlus, X } from "lucide-react";
import Link from "next/link";
import { useState } from "react";

import { Button } from "@/components/ui/button";
import { EndpointPicker } from "@/features/endpoints/components/endpoint-picker";
import type { CatalogEndpoint } from "@/features/endpoints/types";
import type { GraphNode } from "@/features/scenarios/types";
import { cn } from "@/lib/utils";

import { headerProblems, isJson, queryProblems, requestProblems, type Row } from "../request-validation";
import { undefinedNames, type Available } from "../variables";
import { JsonEditor } from "./json-editor";
import type { Suggesting } from "./suggestions";
import { TemplateField } from "./template-field";

const methods = ["GET", "POST", "PUT", "PATCH", "DELETE", "HEAD"];

const input =
  "data h-8 w-full rounded-md border bg-background px-2 text-xs outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:opacity-60";

export type RequestChange = (patch: Partial<GraphNode>, field: string) => void;

function Label({ htmlFor, children }: { htmlFor?: string; children: React.ReactNode }) {
  return (
    <label htmlFor={htmlFor} className="text-[11px] tracking-wider text-muted-foreground uppercase">
      {children}
    </label>
  );
}

function Problem({ id, children }: { id: string; children?: string }) {
  if (!children) return null;
  return (
    <p id={id} role="alert" className="flex items-start gap-1 text-xs text-muted-foreground">
      <CircleAlert aria-hidden className="mt-0.5 size-3 shrink-0" />
      {children}
    </p>
  );
}

// KeyValueRows edits a request's headers or its query: a name and a value a row.
// The rows are held here, blanks included, so that a row being typed does not
// vanish; only the named ones are handed up. Each says what is wrong with it, next
// to it. The value takes {{variables}}, offered as it is typed.
function KeyValueRows({
  label,
  addLabel,
  rows: initial,
  check,
  disabled,
  suggest,
  onChange,
  onBind,
}: {
  label: string;
  addLabel: string;
  rows: Row[];
  // One message per row, for the rows as they stand — blank ones included.
  check: (rows: Row[]) => string[];
  disabled: boolean;
  suggest?: Pick<Suggesting, "available" | "unbound">;
  onChange: (rows: Row[]) => void;
  // Reads a secret into a new step first, then writes the rows built from the
  // variable it was given.
  onBind: (key: string, rowsFor: (variable: string) => Row[]) => void;
}) {
  const [rows, setRows] = useState(initial);

  function change(next: Row[]) {
    setRows(next);
    onChange(next);
  }

  const problems = check(rows);

  return (
    <fieldset className="flex min-w-0 flex-col gap-1.5" disabled={disabled}>
      <legend className="mb-1 text-[11px] tracking-wider text-muted-foreground uppercase">{label}</legend>
      {rows.map((row, index) => {
        const said = problems[index];
        const id = `${label}-${index}-problem`;
        return (
          <div key={index} className="flex flex-col gap-1">
            <div className="flex gap-1.5">
              <input
                aria-label={`${label} name ${index + 1}`}
                aria-invalid={said ? true : undefined}
                aria-describedby={said ? id : undefined}
                value={row.k}
                spellCheck={false}
                onChange={(event) => change(rows.map((r, i) => (i === index ? { ...r, k: event.target.value } : r)))}
                className={cn(input, "min-w-0 flex-1")}
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
            <div className="flex">
              <TemplateField
                label={`${label} value ${index + 1}`}
                value={row.v}
                invalid={Boolean(said)}
                describedBy={said ? id : undefined}
                suggest={
                  suggest && {
                    ...suggest,
                    bind: (key, textFor) =>
                      onBind(key, (variable) => {
                        const next = rows.map((r, i) => (i === index ? { ...r, v: textFor(variable) } : r));
                        setRows(next);
                        return next;
                      }),
                  }
                }
                onChange={(value) => change(rows.map((r, i) => (i === index ? { ...r, v: value } : r)))}
                className={input}
              />
            </div>
            <Problem id={id}>{said}</Problem>
          </div>
        );
      })}
      {!disabled && (
        <Button
          type="button"
          variant="outline"
          size="sm"
          className="w-fit"
          aria-label={addLabel}
          onClick={() => change([...rows, { k: "", v: "" }])}
        >
          + Add
        </Button>
      )}
    </fieldset>
  );
}

// RequestPanel is what an HTTP request node holds, as fields: the method, the path
// after $baseUrl, the headers and the query, a JSON body with its lines numbered
// and coloured, and — from the project's catalog — a whole endpoint at once.
//
// Every field says what is wrong with it where it is. A credential is never typed
// here: a header such as Authorization reads a variable, which a secret of the
// environment fills when the run starts, so there is no value to show.
export function RequestPanel({
  node,
  readOnly,
  endpoints,
  endpointsHref,
  available,
  unbound,
  script,
  onChange,
  onPickEndpoint,
  onBind,
}: {
  node: GraphNode;
  readOnly: boolean;
  // The project's catalog; undefined for somebody who cannot read it.
  endpoints?: CatalogEndpoint[];
  endpointsHref: string;
  available: Available[];
  unbound: string[];
  // The script as it was last saved, shown beside the node so that what a run
  // would execute can be read. It holds references to secrets and no value.
  script: string;
  onChange: RequestChange;
  onPickEndpoint: (endpoint: CatalogEndpoint) => void;
  // Reads a secret into a new step before this node, and applies the patch built
  // from the variable it was given.
  onBind: (key: string, patch: (variable: string) => Partial<GraphNode>) => void;
}) {
  const [picking, setPicking] = useState(false);
  const problems = requestProblems(node);
  const headers: Row[] = Object.entries(node.headers ?? {}).map(([k, v]) => ({ k, v }));
  const query: Row[] = (node.query ?? []).map((q) => ({ k: q.name, v: q.value }));
  const body = node.body ?? "";
  const json = isJson(body, headers);
  const ghosts = undefinedNames(node, available);

  const suggest = readOnly ? undefined : { available, unbound };
  const toHeaders = (rows: Row[]) => Object.fromEntries(rows.filter((r) => r.k.trim() !== "").map((r) => [r.k, r.v]));
  const toQuery = (rows: Row[]) => rows.filter((r) => r.k.trim() !== "").map((r) => ({ name: r.k, value: r.v }));

  return (
    <div className="flex flex-col gap-4">
      {!readOnly && endpoints !== undefined && (
        <div className="flex flex-col gap-2">
          <Button type="button" variant="outline" size="sm" className="w-fit" onClick={() => setPicking(true)} disabled={endpoints.length === 0}>
            <ListPlus aria-hidden />
            Choose from the catalog
          </Button>
          {endpoints.length === 0 && (
            <p className="text-xs text-muted-foreground">
              The catalog is empty.{" "}
              <Link href={endpointsHref} className="text-brand hover:underline">
                Import endpoints
              </Link>
            </p>
          )}
          {picking && (
            <EndpointPicker
              endpoints={endpoints}
              onPick={(endpoint) => {
                onPickEndpoint(endpoint);
                setPicking(false);
              }}
              onClose={() => setPicking(false)}
            />
          )}
        </div>
      )}

      <div className="flex flex-col gap-1">
        <Label htmlFor="request-path">Request</Label>
        <div className="flex gap-1.5">
          <select
            aria-label="Method"
            className={cn(input, "w-24 shrink-0")}
            value={node.method ?? "GET"}
            onChange={(event) => onChange({ method: event.target.value }, "method")}
          >
            {methods.map((m) => (
              <option key={m}>{m}</option>
            ))}
          </select>
          <div className="flex min-w-0 flex-1 items-center rounded-md border bg-background focus-within:ring-2 focus-within:ring-ring">
            <span aria-hidden className="data shrink-0 pl-2 text-xs text-muted-foreground">
              $baseUrl
            </span>
            <input
              id="request-path"
              aria-invalid={problems.path ? true : undefined}
              aria-describedby={problems.path ? "request-path-problem" : "request-path-hint"}
              className="data h-8 min-w-0 flex-1 bg-transparent px-0.5 text-xs outline-none"
              value={node.path ?? ""}
              spellCheck={false}
              onChange={(event) => onChange({ path: event.target.value }, "path")}
            />
          </div>
        </div>
        <Problem id="request-path-problem">{problems.path}</Problem>
        <p id="request-path-hint" className="text-xs text-muted-foreground">
          The address of the environment comes first. A name between braces, as in /users/{"{id}"}, is a variable, and the
          path is the request&apos;s name tag.
        </p>
      </div>

      <KeyValueRows
        label="Headers"
        addLabel="Add a header"
        rows={headers}
        check={headerProblems}
        disabled={readOnly}
        suggest={suggest}
        onChange={(rows) => onChange({ headers: toHeaders(rows) }, "headers")}
        onBind={(key, rowsFor) => onBind(key, (variable) => ({ headers: toHeaders(rowsFor(variable)) }))}
      />

      <KeyValueRows
        label="Query"
        addLabel="Add a query parameter"
        rows={query}
        check={queryProblems}
        disabled={readOnly}
        suggest={suggest}
        onChange={(rows) => onChange({ query: toQuery(rows) }, "query")}
        onBind={(key, rowsFor) => onBind(key, (variable) => ({ query: toQuery(rowsFor(variable)) }))}
      />

      <div className="flex flex-col gap-1">
        <Label htmlFor="request-body">{json ? "Body · json" : "Body"}</Label>
        <JsonEditor
          id="request-body"
          label="Body"
          value={body}
          readOnly={readOnly}
          problem={problems.body}
          describedBy="request-body-problem"
          suggest={suggest && { ...suggest, bind: (key, textFor) => onBind(key, (variable) => ({ body: textFor(variable) })) }}
          onChange={(value) => onChange({ body: value }, "body")}
        />
        <Problem id="request-body-text">{problems.bodyText}</Problem>
        <p className="text-xs text-muted-foreground">{"{{name}} reads a variable. A single brace is text."}</p>
      </div>

      <label className="flex items-center gap-2 text-xs">
        <input type="checkbox" disabled={readOnly} checked={node.critical ?? false} onChange={(e) => onChange({ critical: e.target.checked }, "critical")} />
        Critical: the scenario stops here if this request fails
      </label>

      {ghosts.length > 0 && (
        <p className="flex items-start gap-1 text-xs text-muted-foreground">
          <CircleAlert aria-hidden className="mt-0.5 size-3 shrink-0" />
          <span>
            No step before this one writes{" "}
            {ghosts.map((name, i) => (
              <span key={name}>
                {i > 0 && ", "}
                <code className="data text-foreground">{name}</code>
              </span>
            ))}
            . It reads as undefined unless it comes from elsewhere.
          </span>
        </p>
      )}

      <details className="rounded-md border">
        <summary className="cursor-pointer px-2 py-1.5 text-xs text-muted-foreground">Script preview, as last saved</summary>
        <pre className="data max-h-56 overflow-auto border-t px-2 py-2 text-[11px] leading-4 whitespace-pre">{script}</pre>
        <p className="border-t px-2 py-1.5 text-xs text-muted-foreground">
          A secret appears here as the name it is read by, never as its value.
        </p>
      </details>

    </div>
  );
}
