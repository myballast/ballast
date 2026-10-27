"use client";

import { cn } from "@/lib/utils";

import { kindInfo } from "../kinds";
import type { NodeKind } from "@/features/scenarios/types";
import { KindIcon } from "./kind-icon";

export const dragType = "application/x-ballast-node";

// Palette lists the kinds of node. Each can be dragged onto the canvas, or added
// with Enter or Space: dragging is one way to do it, never the only one. The
// arrow keys move between them, so a keyboard needs one stop here, not eight.
export function Palette({ onAdd }: { onAdd: (kind: NodeKind) => void }) {
  function move(event: React.KeyboardEvent<HTMLUListElement>) {
    const keys: Record<string, number> = { ArrowDown: 1, ArrowRight: 1, ArrowUp: -1, ArrowLeft: -1 };
    const step = keys[event.key];
    if (!step) return;

    const buttons = Array.from(event.currentTarget.querySelectorAll<HTMLButtonElement>("button"));
    const at = buttons.indexOf(document.activeElement as HTMLButtonElement);
    if (at < 0) return;

    event.preventDefault();
    buttons[(at + step + buttons.length) % buttons.length]?.focus();
  }

  return (
    <nav aria-label="Node palette" className="flex flex-col gap-2">
      <h2 className="text-[11px] font-normal tracking-wider text-muted-foreground uppercase">Nodes</h2>
      {/* The arrow keys are handled on the list that holds the buttons. */}
      <ul className="flex flex-col gap-1.5" onKeyDown={move}>
        {kindInfo.map((info) => (
          <li key={info.kind}>
            <button
              type="button"
              draggable
              onDragStart={(event) => {
                event.dataTransfer.setData(dragType, info.kind);
                event.dataTransfer.effectAllowed = "move";
              }}
              onClick={() => onAdd(info.kind)}
              title={info.hint}
              aria-label={`Add ${info.label} node. ${info.hint}`}
              className="flex w-full cursor-grab items-center gap-2 rounded-lg border bg-card px-2 py-1.5 text-left text-[13px] hover:bg-muted active:cursor-grabbing"
            >
              <span className={cn("flex size-5 items-center justify-center rounded", info.tone.tint, info.tone.text)}>
                <KindIcon icon={info.icon} className="size-3.5" />
              </span>
              {info.label}
            </button>
          </li>
        ))}
      </ul>
      <p className="text-xs text-muted-foreground">Drag onto the canvas, or press Enter to add after the selected node.</p>
    </nav>
  );
}
