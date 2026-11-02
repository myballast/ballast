"use client";

import { Plus } from "lucide-react";
import { useActionState, useState, useTransition } from "react";

import { InfoTip } from "@/components/info-tip";
import { Button } from "@/components/ui/button";

import { addThreshold, type ThresholdState } from "../actions";
import {
  buildRule,
  isAdded,
  measures,
  presets,
  ruleOf,
  type EndpointChoice,
  type MeasureId,
} from "../threshold-presets";
import type { Threshold } from "../types";

const field =
  "h-9 rounded-md border bg-background px-3 text-sm outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:opacity-50";

// QuickStart is the rules most scenarios want, one click each. A rule already on
// the scenario is shown as added and cannot be added twice.
export function QuickStart({
  projectId,
  scenarioId,
  thresholds,
}: {
  projectId: string;
  scenarioId: string;
  thresholds: Threshold[];
}) {
  const [pending, startTransition] = useTransition();
  const [error, setError] = useState<string | null>(null);

  function add(rule: { metric: string; expression: string }) {
    setError(null);
    startTransition(async () => {
      const form = new FormData();
      form.set("metric", rule.metric);
      form.set("expression", rule.expression);
      const outcome = await addThreshold(projectId, scenarioId, {}, form);
      if (outcome.error) setError(outcome.error);
    });
  }

  return (
    <section aria-label="Quick start" className="flex flex-col gap-2">
      <h3 className="text-[11px] tracking-wider text-muted-foreground uppercase">Quick start</h3>
      <ul className="grid gap-2 sm:grid-cols-2">
        {presets.map((preset) => {
          const rule = ruleOf(preset);
          const added = isAdded(thresholds, rule);
          return (
            <li key={preset.id} className="flex items-center gap-1.5 rounded-lg border p-2">
              <button
                type="button"
                disabled={added || pending}
                onClick={() => add(rule)}
                className="flex min-w-0 flex-1 items-center gap-2 rounded-md px-2 py-1.5 text-left hover:bg-muted focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none disabled:opacity-60 disabled:hover:bg-transparent"
              >
                <Plus aria-hidden className="size-4 shrink-0 text-muted-foreground" />
                <span className="flex min-w-0 flex-col">
                  <span className="text-sm">{preset.title}</span>
                  <span className="data truncate text-[11px] text-muted-foreground">
                    {rule.metric} · {rule.expression}
                  </span>
                </span>
                {added && <span className="data ml-auto shrink-0 text-[11px] text-muted-foreground">already added</span>}
              </button>
              <InfoTip label={preset.title}>{preset.help}</InfoTip>
            </li>
          );
        })}
      </ul>
      {error && (
        <p role="alert" className="text-sm text-muted-foreground">
          {error}
        </p>
      )}
    </section>
  );
}

// GuidedForm writes a rule from a few choices: what to measure, how, a number and
// maybe one request. It shows the rule in words and as k6 reads it, so that the
// words and the syntax are learnt together.
export function GuidedForm({
  projectId,
  scenarioId,
  endpoints,
}: {
  projectId: string;
  scenarioId: string;
  endpoints: EndpointChoice[];
}) {
  const [measure, setMeasure] = useState<MeasureId>("response_time");
  const [stat, setStat] = useState("p(95)");
  const [value, setValue] = useState(String(measures.response_time.start));
  const [endpoint, setEndpoint] = useState("");

  const spec = measures[measure];
  const built = buildRule({ measure, stat, value: value.trim() === "" ? NaN : Number(value), endpoint });

  const [state, action, pending] = useActionState<ThresholdState, FormData>(async (previous, form) => {
    const next = await addThreshold(projectId, scenarioId, previous, form);
    if (next.done) setValue(String(spec.start));
    return next;
  }, {});

  function choose(next: MeasureId) {
    setMeasure(next);
    setStat(measures[next].stats[0].id);
    setValue(String(measures[next].start));
    if (!measures[next].byEndpoint) setEndpoint("");
  }

  return (
    <form action={action} aria-label="Add a threshold" className="flex flex-col gap-3">
      <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
        <label className="flex flex-col gap-1.5 text-sm">
          Measure
          <select value={measure} onChange={(event) => choose(event.target.value as MeasureId)} className={field}>
            {(Object.keys(measures) as MeasureId[]).map((id) => (
              <option key={id} value={id}>
                {measures[id].label}
              </option>
            ))}
          </select>
        </label>

        <label className="flex flex-col gap-1.5 text-sm">
          For
          <select
            value={stat}
            onChange={(event) => setStat(event.target.value)}
            disabled={spec.stats.length === 1}
            className={field}
          >
            {spec.stats.map((s) => (
              <option key={s.id} value={s.id}>
                {s.label}
              </option>
            ))}
          </select>
        </label>

        <label className="flex flex-col gap-1.5 text-sm">
          {spec.operator === "<" ? "Must stay under" : "Must stay above"}
          <span className="flex items-center gap-1.5">
            <input
              type="number"
              inputMode="decimal"
              min={0}
              step="any"
              value={value}
              onChange={(event) => setValue(event.target.value)}
              aria-describedby="guided-preview"
              className={`data w-full ${field}`}
            />
            <span className="data text-xs text-muted-foreground">{spec.unit}</span>
          </span>
        </label>

        <label className="flex flex-col gap-1.5 text-sm">
          <span className="flex items-center gap-1.5">
            On
            <InfoTip label="the request list">
              Only the requests this scenario sends are listed: a rule on a request it never calls would watch
              nothing. Leave it on all requests to judge the whole run.
            </InfoTip>
          </span>
          <select
            value={endpoint}
            onChange={(event) => setEndpoint(event.target.value)}
            disabled={!spec.byEndpoint}
            className={field}
          >
            <option value="">{spec.byEndpoint ? "All requests" : "Not per request"}</option>
            {endpoints.map((e) => (
              <option key={e.name} value={e.name} disabled={!e.usable}>
                {e.name}
                {e.usable ? "" : " (has a variable)"}
              </option>
            ))}
          </select>
        </label>
      </div>

      {spec.byEndpoint && endpoints.length === 0 && (
        <p className="text-xs text-muted-foreground">
          This scenario is written as code, so Ballast does not know which requests its script sends: the rule applies
          to all requests. To target one, write the rule by hand.
        </p>
      )}

      <div id="guided-preview" aria-live="polite" className="rounded-lg border bg-muted/30 p-3 text-sm">
        {built.ok ? (
          <>
            <p>{built.sentence}.</p>
            <p className="data mt-1 text-xs text-muted-foreground">
              k6: {built.metric} · {built.expression}
            </p>
          </>
        ) : (
          <p className="text-muted-foreground">{built.error}</p>
        )}
      </div>

      {built.ok && (
        <>
          <input type="hidden" name="metric" value={built.metric} />
          <input type="hidden" name="expression" value={built.expression} />
        </>
      )}

      {state.error && (
        <p role="alert" className="text-sm text-muted-foreground">
          {state.error}
        </p>
      )}

      <div>
        <Button type="submit" size="sm" disabled={pending || !built.ok}>
          <Plus aria-hidden />
          {pending ? "Saving…" : "Add threshold"}
        </Button>
      </div>
    </form>
  );
}
