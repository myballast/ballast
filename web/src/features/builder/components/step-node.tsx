"use client";

import { Handle, Position, type Node, type NodeProps } from "@xyflow/react";
import { TriangleAlert } from "lucide-react";
import { memo } from "react";

import { cn } from "@/lib/utils";
import type { GraphNode } from "@/features/scenarios/types";

import { barHelp } from "../help";
import { describe, exits, infoOf } from "../kinds";
import { KindIcon } from "./kind-icon";

export type StepData = {
  node: GraphNode;
  // What is wrong with this node's wiring, if anything.
  issue?: string;
  start?: boolean;
  // An error is on this step: it is drawn with a heavier frame, besides the words it
  // already says, so that it can be found among forty.
  flagged?: boolean;
};
export type StepFlowNode = Node<StepData, "step">;

const label = "data absolute -top-2.5 rounded border bg-background px-1 text-[10px] text-muted-foreground";

// StepNode is one node of the canvas. It says its kind in words and an icon, its
// title and what it holds, and — in words, with an icon — when its wiring is
// incomplete. Colour tells none of it.
function StepNodeView({ data, selected }: NodeProps<StepFlowNode>) {
  const { node, issue, start, flagged } = data;
  const info = infoOf(node.kind);
  const { title, sub } = describe(node);
  const ways = exits(node.kind);

  return (
    <div
      className={cn(
        "relative w-52 rounded-[10px] border bg-card py-3 pr-3.5 pl-4 text-left",
        flagged && "border-2 border-foreground/70",
        selected && "border-brand ring-2 ring-ring",
      )}
    >
      <span aria-hidden className={cn("absolute inset-y-0 left-0 w-[3px] rounded-l-[10px]", info.tone.bar)} />
      <Handle type="target" position={Position.Left} />

      <div className="flex items-center gap-1.5 text-[11px] text-muted-foreground">
        <span className={cn("flex size-5 items-center justify-center rounded", info.tone.tint, info.tone.text)}>
          <KindIcon icon={info.icon} className="size-3.5" />
        </span>
        <span className="tracking-wider uppercase">{info.label}</span>
        {start && (
          <span className="data ml-auto rounded border px-1 text-[10px]" title={barHelp.start}>
            start
          </span>
        )}
      </div>
      <div className="data mt-2 truncate text-[13px]" title={title}>
        {title}
      </div>
      {sub && (
        <div className="mt-1 truncate text-xs text-muted-foreground" title={sub}>
          {sub}
        </div>
      )}
      {issue && (
        <div className="mt-1.5 flex items-center gap-1 text-[11px] text-muted-foreground">
          <TriangleAlert aria-hidden className="size-3 shrink-0" />
          <span>{issue}</span>
        </div>
      )}

      {node.kind === "end" ? null : ways.length === 0 ? (
        <Handle type="source" position={Position.Right} />
      ) : (
        ways.map((way, index) => (
          <Handle
            key={way}
            id={way}
            type="source"
            position={Position.Right}
            style={{ top: index === 0 ? "30%" : "70%" }}
          >
            <span className={cn(label, "pointer-events-none left-2 -translate-y-1/2 top-1/2")}>{way}</span>
          </Handle>
        ))
      )}
    </div>
  );
}

export const StepNode = memo(StepNodeView);
