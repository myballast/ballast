"use client";

import { CircleAlert } from "lucide-react";
import { useId, useState } from "react";

import type { GraphNode } from "@/features/scenarios/types";

import { loopProblem, maxLoop, maxPause, pauseProblem, setVariableProblem } from "../node-validation";
import { ConditionEditor } from "./condition-editor";

const input =
  "data h-8 w-full rounded-md border bg-background px-2 text-xs outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:opacity-60";

type Change = (patch: Partial<GraphNode>, field: string) => void;

function Problem({ children }: { children?: string }) {
  if (!children) return null;
  return (
    <p role="alert" className="flex items-start gap-1 text-xs text-muted-foreground">
      <CircleAlert aria-hidden className="mt-0.5 size-3 shrink-0" />
      {children}
    </p>
  );
}

function Label({ htmlFor, children }: { htmlFor: string; children: React.ReactNode }) {
  return (
    <label htmlFor={htmlFor} className="text-[11px] tracking-wider text-muted-foreground uppercase">
      {children}
    </label>
  );
}

// Both numbers a person types are whole or decimal numbers, or nothing yet.
const whole = (text: string) => (text === "" ? 0 : Number.parseInt(text, 10) || 0);
const decimal = (text: string) => (text === "" ? 0 : Number.parseFloat(text) || 0);

// ConditionPanel is a condition: what to test, and which way each answer goes. The
// two ways out are the true and false handles of the node, said here so nobody has
// to guess which wire is which.
export function ConditionPanel({
  node,
  readOnly,
  available,
  onChange,
}: {
  node: GraphNode;
  readOnly: boolean;
  available: string[];
  onChange: Change;
}) {
  return (
    <div className="flex flex-col gap-3">
      <ConditionEditor
        label="Test"
        value={node.expression ?? ""}
        available={available}
        readOnly={readOnly}
        onChange={(expression) => onChange({ expression }, "expression")}
      />
      <p className="text-xs text-muted-foreground">
        Two ways out: <span className="data text-foreground">true</span> when it holds,{" "}
        <span className="data text-foreground">false</span> when it does not. Both have to lead somewhere.
      </p>
    </div>
  );
}

// AssertionPanel is a check on what just came back — its status, how long it took,
// or something read from it — and what a failed one does: count it in the run's
// checks and carry on, or stop the iteration.
export function AssertionPanel({
  node,
  readOnly,
  available,
  onChange,
}: {
  node: GraphNode;
  readOnly: boolean;
  available: string[];
  onChange: Change;
}) {
  const name = useId();
  const stops = node.critical ?? false;

  return (
    <div className="flex flex-col gap-3">
      <ConditionEditor
        label="Check"
        value={node.expression ?? ""}
        available={available}
        readOnly={readOnly}
        onChange={(expression) => onChange({ expression }, "expression")}
      />

      <fieldset className="flex flex-col gap-1.5" disabled={readOnly}>
        <legend className="mb-1 text-[11px] tracking-wider text-muted-foreground uppercase">If it does not hold</legend>
        <label className="flex items-start gap-2 text-xs">
          <input type="radio" name={name} checked={!stops} onChange={() => onChange({ critical: false }, "critical")} />
          <span>
            Count only: the run&apos;s checks record it, and the scenario carries on.
          </span>
        </label>
        <label className="flex items-start gap-2 text-xs">
          <input type="radio" name={name} checked={stops} onChange={() => onChange({ critical: true }, "critical")} />
          <span>Stop: the iteration ends here, and the next steps are not run.</span>
        </label>
      </fieldset>
    </div>
  );
}

// LoopPanel repeats a few steps, a number of times or once for each element of a
// list. A maximum is asked for either way: a list somebody else's server returns
// has no length this scenario can promise, and the maximum is what bounds the run.
export function LoopPanel({
  node,
  readOnly,
  available,
  onChange,
}: {
  node: GraphNode;
  readOnly: boolean;
  available: string[];
  onChange: Change;
}) {
  const id = useId();
  const overList = node.over !== undefined && node.over !== "";
  const mode = overList || node.item ? "list" : "count";
  const problem = loopProblem(node);

  return (
    <div className="flex flex-col gap-4">
      <fieldset className="flex flex-col gap-1.5" disabled={readOnly}>
        <legend className="mb-1 text-[11px] tracking-wider text-muted-foreground uppercase">Repeat</legend>
        <label className="flex items-center gap-2 text-xs">
          <input
            type="radio"
            name={`${id}-mode`}
            checked={mode === "count"}
            // A count has no list: leaving one behind would be refused as half of a pair.
            onChange={() => onChange({ over: "", item: "" }, "mode")}
          />
          A number of times
        </label>
        <label className="flex items-center gap-2 text-xs">
          <input
            type="radio"
            name={`${id}-mode`}
            checked={mode === "list"}
            onChange={() => onChange({ over: available[0] ?? "items", item: "item" }, "mode")}
          />
          Once for each element of a list
        </label>
      </fieldset>

      {mode === "list" && (
        <>
          <datalist id={`${id}-lists`}>
            {available.map((name) => (
              <option key={name} value={name} />
            ))}
          </datalist>
          <div className="flex flex-col gap-1">
            <Label htmlFor={`${id}-over`}>List</Label>
            <input
              id={`${id}-over`}
              list={`${id}-lists`}
              className={input}
              value={node.over ?? ""}
              disabled={readOnly}
              spellCheck={false}
              autoComplete="off"
              onChange={(event) => onChange({ over: event.target.value }, "over")}
            />
            <p className="text-xs text-muted-foreground">
              A variable an earlier step extracted from the body, where the path points at a list.
            </p>
          </div>
          <div className="flex flex-col gap-1">
            <Label htmlFor={`${id}-item`}>Each element goes in</Label>
            <input
              id={`${id}-item`}
              className={input}
              value={node.item ?? ""}
              disabled={readOnly}
              spellCheck={false}
              onChange={(event) => onChange({ item: event.target.value }, "item")}
            />
          </div>
        </>
      )}

      <div className="flex flex-col gap-1">
        <Label htmlFor={`${id}-max`}>{mode === "list" ? "At most" : "Times"}</Label>
        <input
          id={`${id}-max`}
          type="number"
          min={1}
          max={maxLoop}
          className={input}
          value={node.iterations ?? ""}
          disabled={readOnly}
          aria-invalid={problem?.includes("maximum") ? true : undefined}
          onChange={(event) => onChange({ iterations: whole(event.target.value) }, "iterations")}
        />
        <p className="text-xs text-muted-foreground">
          {mode === "list"
            ? `The most elements it takes, from 1 to ${maxLoop}. A longer list is cut there.`
            : `From 1 to ${maxLoop}.`}{" "}
          Two ways out: <span className="data text-foreground">body</span> is what repeats and comes back to this step,{" "}
          <span className="data text-foreground">after</span> is where it goes next.
        </p>
      </div>

      <Problem>{problem}</Problem>
    </div>
  );
}

// PausePanel waits as long as a person would: a fixed time, or a time picked
// between two bounds each time round, so that a hundred virtual users do not all
// wake at once.
export function PausePanel({ node, readOnly, onChange }: { node: GraphNode; readOnly: boolean; onChange: Change }) {
  const id = useId();
  const range = (node.seconds_max ?? 0) > 0;
  const problem = pauseProblem(node);

  return (
    <div className="flex flex-col gap-4">
      <fieldset className="flex flex-col gap-1.5" disabled={readOnly}>
        <legend className="mb-1 text-[11px] tracking-wider text-muted-foreground uppercase">Wait</legend>
        <label className="flex items-center gap-2 text-xs">
          <input type="radio" name={`${id}-mode`} checked={!range} onChange={() => onChange({ seconds_max: 0 }, "mode")} />
          A fixed time
        </label>
        <label className="flex items-center gap-2 text-xs">
          <input
            type="radio"
            name={`${id}-mode`}
            checked={range}
            onChange={() => onChange({ seconds_max: Math.min(maxPause, (node.seconds ?? 1) * 2) }, "mode")}
          />
          A time picked between two bounds
        </label>
      </fieldset>

      <div className="flex gap-2">
        <div className="flex min-w-0 flex-1 flex-col gap-1">
          <Label htmlFor={`${id}-min`}>{range ? "Shortest (s)" : "Seconds"}</Label>
          <input
            id={`${id}-min`}
            type="number"
            min={0}
            step="0.1"
            className={input}
            value={node.seconds ?? ""}
            disabled={readOnly}
            onChange={(event) => onChange({ seconds: decimal(event.target.value) }, "seconds")}
          />
        </div>
        {range && (
          <div className="flex min-w-0 flex-1 flex-col gap-1">
            <Label htmlFor={`${id}-max`}>Longest (s)</Label>
            <input
              id={`${id}-max`}
              type="number"
              min={0}
              step="0.1"
              className={input}
              value={node.seconds_max ?? ""}
              disabled={readOnly}
              onChange={(event) => onChange({ seconds_max: decimal(event.target.value) }, "seconds_max")}
            />
          </div>
        )}
      </div>
      <p className="text-xs text-muted-foreground">Between 0 and {maxPause} seconds.</p>
      <Problem>{problem}</Problem>
    </div>
  );
}

// SetVariablePanel gives a variable a value, or reads it from a secret of the
// environment — the key of the secret, never what it holds.
export function SetVariablePanel({ node, readOnly, onChange }: { node: GraphNode; readOnly: boolean; onChange: Change }) {
  const id = useId();
  // Held here: a secret whose key is still being typed has an empty key, which the
  // node cannot tell from "a value".
  const [secret, setSecret] = useState(Boolean(node.from_env));
  const problem = setVariableProblem(node);

  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-col gap-1">
        <Label htmlFor={`${id}-name`}>Variable</Label>
        <input
          id={`${id}-name`}
          className={input}
          value={node.variable ?? ""}
          disabled={readOnly}
          spellCheck={false}
          onChange={(event) => onChange({ variable: event.target.value }, "variable")}
        />
      </div>

      <div className="flex flex-col gap-1">
        <Label htmlFor={`${id}-from`}>Taken from</Label>
        <select
          id={`${id}-from`}
          className={input}
          value={secret ? "secret" : "value"}
          disabled={readOnly}
          onChange={(event) => {
            const next = event.target.value === "secret";
            setSecret(next);
            // One or the other: the API refuses a node that holds both.
            onChange(next ? { value: "", from_env: "" } : { from_env: "" }, "from");
          }}
        >
          <option value="value">a value</option>
          <option value="secret">a secret of the environment</option>
        </select>
      </div>

      {secret ? (
        <div className="flex flex-col gap-1">
          <Label htmlFor={`${id}-key`}>Secret key</Label>
          <input
            id={`${id}-key`}
            className={input}
            value={node.from_env ?? ""}
            disabled={readOnly}
            spellCheck={false}
            placeholder="API_TOKEN"
            onChange={(event) => onChange({ from_env: event.target.value }, "from_env")}
          />
          <p className="text-xs text-muted-foreground">
            Read when the run starts. The value is never written in the script, and is never shown.
          </p>
        </div>
      ) : (
        <div className="flex flex-col gap-1">
          <Label htmlFor={`${id}-value`}>Value</Label>
          <input
            id={`${id}-value`}
            className={input}
            value={node.value ?? ""}
            disabled={readOnly}
            spellCheck={false}
            onChange={(event) => onChange({ value: event.target.value }, "value")}
          />
          <p className="text-xs text-muted-foreground">{"{{name}} reads another variable. A single brace is text."}</p>
        </div>
      )}

      <Problem>{problem ?? (secret && !node.from_env ? "Give the key of the secret, such as API_TOKEN." : undefined)}</Problem>
    </div>
  );
}
