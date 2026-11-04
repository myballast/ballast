"use client";

import { CircleAlert, X } from "lucide-react";
import { useId, useMemo, useState } from "react";

import { InfoTip } from "@/components/info-tip";
import { Button } from "@/components/ui/button";

import { fromGuided, operatorLabels, responseStatus, responseTimeMs, toGuided, type Clause, type Guided, type Operator } from "../condition";
import { expressionProblem } from "../node-validation";

const input =
  "data h-8 w-full rounded-md border bg-background px-2 text-xs outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:opacity-60";

const operators = Object.keys(operatorLabels) as Operator[];

const blank: Clause = { variable: "", op: "==", value: "", text: false };

// ConditionEditor writes an expression the way it is said: "this variable, is at
// least, this value", one row to a comparison, the rows joined by all or any. What
// it writes is the grammar's own text, shown and editable below the rows — never
// JavaScript — and what the form cannot say (a mix of both joins, a group) is left
// as that text, said plainly, rather than rewritten.
//
// A mistake in the text is named where it is, here, instead of at the end of a run.
export function ConditionEditor({
  value,
  onChange,
  available,
  readOnly,
  label,
  help,
}: {
  value: string;
  onChange: (expression: string) => void;
  // The variables the step can read. The two the response owns are always offered.
  available: string[];
  readOnly: boolean;
  label: string;
  // What the field means, for the "i" beside its title.
  help?: string;
}) {
  const id = useId();
  const listId = `${id}-variables`;

  const parsed = useMemo(() => toGuided(value), [value]);
  // Rows being typed may be unfinished, which the text cannot hold: they live
  // here until the text takes over again.
  const [draft, setDraft] = useState<Guided | null>(null);
  const guided = draft ?? parsed;
  const problem = expressionProblem(value);

  function write(next: Guided) {
    setDraft(next);
    onChange(fromGuided(next));
  }

  function row(index: number, patch: Partial<Clause>) {
    if (!guided) return;
    write({ ...guided, clauses: guided.clauses.map((c, i) => (i === index ? { ...c, ...patch } : c)) });
  }

  const names = [...new Set([responseStatus, responseTimeMs, ...available])];

  return (
    <div className="flex flex-col gap-3">
      <datalist id={listId}>
        {names.map((name) => (
          <option key={name} value={name} />
        ))}
      </datalist>

      {guided ? (
        <fieldset className="flex min-w-0 flex-col gap-2" disabled={readOnly}>
          <legend className="mb-1 flex items-center gap-1.5 text-[11px] tracking-wider text-muted-foreground uppercase">
            {label}
            {help && <InfoTip label={label}>{help}</InfoTip>}
          </legend>

          {guided.clauses.length > 1 && (
            <label className="flex items-center gap-2 text-xs text-muted-foreground">
              It holds when
              <select
                className={`${input} w-auto`}
                value={guided.join}
                onChange={(event) => write({ ...guided, join: event.target.value as Guided["join"] })}
              >
                <option value="all">all of these hold</option>
                <option value="any">any of these holds</option>
              </select>
            </label>
          )}

          {guided.clauses.map((clause, index) => (
            <div key={index} className="flex flex-col gap-1.5 rounded-md border p-2">
              <div className="flex gap-1.5">
                <input
                  aria-label={`Variable ${index + 1}`}
                  list={listId}
                  className={input}
                  value={clause.variable}
                  spellCheck={false}
                  autoComplete="off"
                  onChange={(event) => row(index, { variable: event.target.value })}
                />
                <Button
                  type="button"
                  variant="ghost"
                  size="icon-sm"
                  aria-label={`Remove condition ${index + 1}`}
                  disabled={guided.clauses.length === 1}
                  onClick={() => write({ ...guided, clauses: guided.clauses.filter((_, i) => i !== index) })}
                >
                  <X aria-hidden />
                </Button>
              </div>

              <div className="flex gap-1.5">
                <select
                  aria-label={`Comparison ${index + 1}`}
                  className={input}
                  value={clause.op}
                  onChange={(event) => row(index, { op: event.target.value as Operator | "" })}
                >
                  {operators.map((op) => (
                    <option key={op} value={op}>
                      {operatorLabels[op]}
                    </option>
                  ))}
                  <option value="">is true</option>
                </select>

                {clause.op !== "" && (
                  <>
                    <select
                      aria-label={`Kind of value ${index + 1}`}
                      className={`${input} w-24 shrink-0`}
                      value={clause.text ? "text" : "number"}
                      onChange={(event) => row(index, { text: event.target.value === "text" })}
                    >
                      <option value="number">number</option>
                      <option value="text">text</option>
                    </select>
                    <input
                      aria-label={`Value ${index + 1}`}
                      className={input}
                      value={clause.value}
                      spellCheck={false}
                      onChange={(event) => row(index, { value: event.target.value })}
                    />
                  </>
                )}
              </div>
            </div>
          ))}

          {!readOnly && (
            <Button
              type="button"
              variant="outline"
              size="sm"
              className="w-fit"
              onClick={() => write({ ...guided, clauses: [...guided.clauses, { ...blank, variable: "" }] })}
            >
              + Add a condition
            </Button>
          )}
        </fieldset>
      ) : (
        <p className="flex items-start gap-1 text-xs text-muted-foreground">
          <CircleAlert aria-hidden className="mt-0.5 size-3 shrink-0" />
          <span>
            {value.trim() === "" || problem
              ? "The form comes back as soon as the expression below can be read."
              : "This expression mixes && and || or uses a group, which the form cannot show. Edit it as text below."}
          </span>
        </p>
      )}

      <div className="flex flex-col gap-1">
        <label htmlFor={`${id}-text`} className="text-[11px] tracking-wider text-muted-foreground uppercase">
          Expression
        </label>
        <input
          id={`${id}-text`}
          className={input}
          value={value}
          disabled={readOnly}
          spellCheck={false}
          aria-invalid={problem ? true : undefined}
          aria-describedby={`${id}-problem`}
          onChange={(event) => {
            // The text is the truth now; the rows follow it, if they can.
            setDraft(null);
            onChange(event.target.value);
          }}
        />
        {problem ? (
          <p id={`${id}-problem`} role="alert" className="flex items-start gap-1 text-xs text-muted-foreground">
            <CircleAlert aria-hidden className="mt-0.5 size-3 shrink-0" />
            {problem}
          </p>
        ) : (
          <p id={`${id}-problem`} className="text-xs text-muted-foreground">
            Comparisons joined by && or ||. Never code. response_status and response_time_ms read the last response.
          </p>
        )}
      </div>
    </div>
  );
}
