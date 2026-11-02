"use client";

import { Info } from "lucide-react";
import { useEffect, useId, useLayoutEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";

import { cn } from "@/lib/utils";

// InfoTip is a small "i" that explains the word next to it. It opens on a click, on
// the keyboard and on hover, and closes on Escape or a click elsewhere. The text
// is said to a screen reader through the button, so that nothing is only on hover.
//
// The bubble is drawn on the page's body, not inside the card: a card clips what
// overflows it, and a bubble cut at the card's edge says nothing.
export function InfoTip({ label, children, className }: { label: string; children: string; className?: string }) {
  const [open, setOpen] = useState(false);
  const id = useId();
  const root = useRef<HTMLSpanElement>(null);
  const bubble = useRef<HTMLSpanElement>(null);
  const [at, setAt] = useState<{ top: number; left: number } | null>(null);

  // Under the button, kept inside the window.
  useLayoutEffect(() => {
    if (!open || !root.current) return;
    const rect = root.current.getBoundingClientRect();
    const width = 240;
    setAt({ top: rect.bottom + 6, left: Math.max(8, Math.min(rect.left, window.innerWidth - width - 8)) });
  }, [open]);

  useEffect(() => {
    if (!open) return;
    function onKey(event: KeyboardEvent) {
      if (event.key === "Escape") setOpen(false);
    }
    function onPointer(event: PointerEvent) {
      const target = event.target as Node;
      if (!root.current?.contains(target) && !bubble.current?.contains(target)) setOpen(false);
    }
    const close = () => setOpen(false);
    window.addEventListener("keydown", onKey);
    window.addEventListener("pointerdown", onPointer);
    window.addEventListener("scroll", close, true);
    window.addEventListener("resize", close);
    return () => {
      window.removeEventListener("scroll", close, true);
      window.removeEventListener("resize", close);
      window.removeEventListener("keydown", onKey);
      window.removeEventListener("pointerdown", onPointer);
    };
  }, [open]);

  return (
    <span
      ref={root}
      className={cn("relative inline-flex normal-case", className)}
      onMouseEnter={() => setOpen(true)}
      onMouseLeave={() => setOpen(false)}
    >
      <button
        type="button"
        aria-label={`What is ${label}?`}
        aria-expanded={open}
        aria-controls={id}
        onClick={() => setOpen(true)}
        className="grid size-4 place-items-center rounded-full text-muted-foreground hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none"
      >
        <Info aria-hidden className="size-3.5" />
      </button>
      {open &&
        at &&
        createPortal(
          <span
            ref={bubble}
            id={id}
            role="note"
            style={{ top: at.top, left: at.left }}
            className="fixed z-50 w-60 rounded-md border bg-popover p-2.5 text-xs leading-relaxed font-normal tracking-normal text-popover-foreground normal-case"
          >
            {children}
          </span>,
          document.body,
        )}
    </span>
  );
}
