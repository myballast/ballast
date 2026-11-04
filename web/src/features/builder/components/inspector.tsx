"use client";

import { Trash2, X } from "lucide-react";
import { useId } from "react";

import { InfoTip } from "@/components/info-tip";
import { Button } from "@/components/ui/button";
import type { GraphNode } from "@/features/scenarios/types";
import { cn } from "@/lib/utils";

import { fieldHelp, kindHelp } from "../help";
import { describe, infoOf } from "../kinds";
import type { Available } from "../variables";
import { AvailableVariables } from "./available-variables";
import { ExtractPanel } from "./extract-panel";
import { AssertionPanel, ConditionPanel, LoopPanel, PausePanel, SetVariablePanel } from "./flow-panels";
import { KindIcon } from "./kind-icon";
import { RequestPanel } from "./request-panel";

const input =
  "data h-8 w-full rounded-md border bg-background px-2 text-xs outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:opacity-60";

function Field({ label, hint, help, children }: { label: string; hint?: string; help?: string; children: (id: string) => React.ReactNode }) {
  const id = useId();
  return (
    <div className="flex flex-col gap-1">
      <span className="flex items-center gap-1.5">
        <label htmlFor={id} className="text-[11px] tracking-wider text-muted-foreground uppercase">
          {label}
        </label>
        {help && <InfoTip label={label}>{help}</InfoTip>}
      </span>
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
  const names = available.map((variable) => variable.name);

  return (
    <aside aria-label="Node inspector" className="flex h-full flex-col overflow-hidden rounded-xl border bg-card">
      <div className="flex items-center gap-2 border-b px-3 py-2.5">
        <span className={cn("flex size-6 items-center justify-center rounded-md", info.tone.tint, info.tone.text)}>
          <KindIcon icon={info.icon} className="size-3.5" />
        </span>
        <div className="min-w-0 flex-1">
          <div className="flex items-center gap-1.5 text-sm">
            <span className="truncate">{info.label}</span>
            <InfoTip label={info.label}>{kindHelp[node.kind]}</InfoTip>
          </div>
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
          <Field label="Label" help={fieldHelp.label} hint="A name for people. It is written in the script as a comment.">
            {(id) => (
              <input id={id} className={input} value={node.label ?? ""} onChange={(e) => onChange({ label: e.target.value }, "label")} />
            )}
          </Field>

          {node.kind === "http_request" && <RequestPanel node={node} readOnly={readOnly} onChange={onChange} {...request} />}

          {node.kind === "extract_variable" && <ExtractPanel node={node} readOnly={readOnly} onChange={onChange} />}

          {node.kind === "set_variable" && <SetVariablePanel node={node} readOnly={readOnly} onChange={onChange} />}

          {node.kind === "condition" && (
            <ConditionPanel node={node} readOnly={readOnly} available={names} onChange={onChange} />
          )}

          {node.kind === "assertion" && (
            <AssertionPanel node={node} readOnly={readOnly} available={names} onChange={onChange} />
          )}

          {node.kind === "loop" && <LoopPanel node={node} readOnly={readOnly} available={names} onChange={onChange} />}

          {node.kind === "think_time" && <PausePanel node={node} readOnly={readOnly} onChange={onChange} />}

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
