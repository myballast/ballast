"use client";

import { CircleHelp, X } from "lucide-react";
import { useEffect, useId, useRef, useState } from "react";

import { Button } from "@/components/ui/button";

import { howItWorks, shortcuts } from "../help";

// BuilderHelp is the "?" over the canvas: how the builder works in four steps, and
// its shortcuts. It opens when asked and never on its own.
export function BuilderHelp() {
  const [open, setOpen] = useState(false);
  const root = useRef<HTMLDivElement>(null);
  const id = useId();

  useEffect(() => {
    if (!open) return;
    const onKey = (event: KeyboardEvent) => {
      if (event.key === "Escape") setOpen(false);
    };
    const onPointer = (event: PointerEvent) => {
      if (!root.current?.contains(event.target as Node)) setOpen(false);
    };
    window.addEventListener("keydown", onKey);
    window.addEventListener("pointerdown", onPointer);
    return () => {
      window.removeEventListener("keydown", onKey);
      window.removeEventListener("pointerdown", onPointer);
    };
  }, [open]);

  return (
    <div ref={root} className="relative">
      <Button
        variant="outline"
        size="icon"
        aria-label="How the builder works"
        aria-expanded={open}
        aria-controls={id}
        onClick={() => setOpen((o) => !o)}
      >
        <CircleHelp aria-hidden />
      </Button>

      {open && (
        <div
          id={id}
          role="dialog"
          aria-label="How the builder works"
          className="absolute top-full left-0 z-30 mt-2 flex max-h-[70svh] w-[min(24rem,calc(100vw-3rem))] flex-col gap-3 overflow-y-auto rounded-xl border bg-popover p-4 text-sm text-popover-foreground"
        >
          <div className="flex items-start justify-between gap-3">
            <h2 className="text-base font-normal tracking-tight">How the builder works</h2>
            <button
              type="button"
              onClick={() => setOpen(false)}
              className="grid size-6 place-items-center rounded-md text-muted-foreground hover:bg-muted hover:text-foreground"
            >
              <X aria-hidden className="size-4" />
              <span className="sr-only">Close</span>
            </button>
          </div>

          <ol className="flex flex-col gap-2.5">
            {howItWorks.map((step, index) => (
              <li key={step.title} className="flex gap-2.5">
                <span aria-hidden className="data mt-0.5 grid size-5 shrink-0 place-items-center rounded-full border text-[11px] text-muted-foreground">
                  {index + 1}
                </span>
                <span className="flex flex-col gap-0.5">
                  <span>{step.title}</span>
                  <span className="text-xs leading-relaxed text-muted-foreground">{step.text}</span>
                </span>
              </li>
            ))}
          </ol>

          <div className="flex flex-col gap-1.5 border-t pt-3">
            <h3 className="text-[11px] tracking-wider text-muted-foreground uppercase">Shortcuts</h3>
            <dl className="flex flex-col gap-1.5">
              {shortcuts.map((shortcut) => (
                <div key={shortcut.keys} className="flex items-baseline justify-between gap-3 text-xs">
                  <dd className="text-muted-foreground">{shortcut.does}</dd>
                  <dt className="data shrink-0 rounded border px-1.5 py-0.5 text-[11px]">{shortcut.keys}</dt>
                </div>
              ))}
            </dl>
          </div>
        </div>
      )}
    </div>
  );
}
