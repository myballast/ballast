"use client";

import { Gauge, Pencil, Plus, Trash2 } from "lucide-react";
import { useActionState, useState } from "react";

import { ConfirmAction } from "@/components/confirm-action";
import { EmptyState } from "@/components/states";
import { Button } from "@/components/ui/button";

import { addThreshold, deleteThreshold, updateThreshold, type ThresholdState } from "../actions";
import type { Threshold } from "../types";

const field =
  "h-9 rounded-md border bg-background px-3 text-sm outline-none focus-visible:ring-2 focus-visible:ring-ring";

// ThresholdRules is the rules a scenario imposes, each a k6 metric and an
// expression. Whoever may not change them sees the list alone.
//
// Changing or removing a rule never touches a run already recorded: its
// verdicts were frozen when it finished.
export function ThresholdRules({
  projectId,
  scenarioId,
  thresholds,
  editable,
}: {
  projectId: string;
  scenarioId: string;
  thresholds: Threshold[];
  editable: boolean;
}) {
  const [editing, setEditing] = useState<string | null>(null);

  return (
    <div className="flex flex-col gap-4">
      {thresholds.length === 0 ? (
        <EmptyState
          icon={Gauge}
          title="No threshold"
          description="A threshold is a rule k6 judges at the end of a run, such as p(95)<300 on http_req_duration. Without one, a run passes as long as it ends."
          action={
            editable ? (
              // The form is the next thing on the page; the link takes the keyboard
              // to its first field, where a button that only scrolled would not.
              <Button
                onClick={() => document.getElementById("new-threshold")?.querySelector("input")?.focus()}
              >
                Add a threshold
              </Button>
            ) : undefined
          }
        />
      ) : (
        <ul className="flex flex-col divide-y rounded-xl border">
          {thresholds.map((t) =>
            editable && editing === t.id ? (
              <li key={t.id} className="px-4 py-3">
                <RuleForm
                  submit={updateThreshold.bind(null, projectId, scenarioId, t.id)}
                  initial={t}
                  label="Save"
                  onDone={() => setEditing(null)}
                  onCancel={() => setEditing(null)}
                />
              </li>
            ) : (
              <li key={t.id} className="flex flex-wrap items-center justify-between gap-3 px-4 py-3 text-sm">
                <span className="flex min-w-0 flex-wrap items-baseline gap-x-3 gap-y-1">
                  <code className="data break-all">{t.metric}</code>
                  <code className="data text-muted-foreground">{t.expression}</code>
                </span>
                {editable && (
                  <span className="flex flex-wrap gap-2">
                    <Button variant="ghost" size="sm" onClick={() => setEditing(t.id)}>
                      <Pencil aria-hidden />
                      Edit
                    </Button>
                    <ConfirmAction
                      icon={<Trash2 aria-hidden />}
                      label="Delete"
                      variant="destructive"
                      question={
                        <>
                          Delete <code className="data">{t.metric}</code>{" "}
                          <code className="data">{t.expression}</code>? Runs already recorded keep their verdicts.
                        </>
                      }
                      confirm="Delete threshold"
                      busy="Deleting…"
                      act={deleteThreshold.bind(null, projectId, scenarioId, t.id)}
                    />
                  </span>
                )}
              </li>
            ),
          )}
        </ul>
      )}

      {editable && (
        <div id="new-threshold" className="rounded-xl border p-4">
          <RuleForm submit={addThreshold.bind(null, projectId, scenarioId)} label="Add threshold" clearOnDone />
        </div>
      )}
    </div>
  );
}

function RuleForm({
  submit,
  initial,
  label,
  clearOnDone = false,
  onDone,
  onCancel,
}: {
  submit: (previous: ThresholdState, form: FormData) => Promise<ThresholdState>;
  initial?: Threshold;
  label: string;
  clearOnDone?: boolean;
  onDone?: () => void;
  onCancel?: () => void;
}) {
  const [metric, setMetric] = useState(initial?.metric ?? "");
  const [expression, setExpression] = useState(initial?.expression ?? "");
  const [state, action, pending] = useActionState<ThresholdState, FormData>(async (previous, form) => {
    const next = await submit(previous, form);
    if (next.done) {
      if (clearOnDone) {
        setMetric("");
        setExpression("");
      }
      onDone?.();
    }
    return next;
  }, {});

  return (
    <form action={action} aria-label={label} className="flex flex-col gap-3">
      <div className="grid gap-3 sm:grid-cols-[2fr_1fr]">
        <label className="flex flex-col gap-1.5 text-sm">
          Metric
          <input
            name="metric"
            required
            maxLength={200}
            value={metric}
            onChange={(event) => setMetric(event.target.value)}
            placeholder="http_req_duration{name:POST /login}"
            spellCheck={false}
            className={`data ${field}`}
          />
        </label>
        <label className="flex flex-col gap-1.5 text-sm">
          Expression
          <input
            name="expression"
            required
            maxLength={120}
            value={expression}
            onChange={(event) => setExpression(event.target.value)}
            placeholder="p(95)<300"
            spellCheck={false}
            className={`data ${field}`}
          />
        </label>
      </div>
      {!initial && (
        <span className="text-xs text-muted-foreground">
          A k6 metric, optionally narrowed to one endpoint by its{" "}
          <code className="data text-foreground">name</code> tag:{" "}
          <code className="data text-foreground">http_req_duration{"{name:POST /login}"}</code>. The expression is
          k6&apos;s: <code className="data text-foreground">p(95)&lt;300</code>,{" "}
          <code className="data text-foreground">rate&lt;0.01</code>.
        </span>
      )}
      {state.error && (
        <p role="alert" className="text-sm text-muted-foreground">
          {state.error}
        </p>
      )}
      <div className="flex gap-2">
        <Button type="submit" size="sm" disabled={pending}>
          {!initial && <Plus aria-hidden />}
          {pending ? "Saving…" : label}
        </Button>
        {onCancel && (
          <Button type="button" variant="ghost" size="sm" onClick={onCancel} disabled={pending}>
            Cancel
          </Button>
        )}
      </div>
    </form>
  );
}
