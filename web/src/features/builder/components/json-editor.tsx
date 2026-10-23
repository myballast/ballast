"use client";

import { useRef, useState } from "react";

import { cn } from "@/lib/utils";

import { tokenizeJsonLine, type Token } from "../json-highlight";
import type { JsonProblem } from "../request-validation";
import { useSuggestions, type Suggesting } from "./suggestions";

const kindClass: Record<Token["kind"], string> = {
  key: "text-syntax-key",
  string: "text-syntax-string",
  number: "text-syntax-number",
  literal: "text-syntax-number",
  variable: "text-syntax-variable font-medium",
  punctuation: "text-muted-foreground",
  plain: "",
};

// JsonEditor is a textarea with the body drawn under it: numbered lines, and each
// name, string, number and {{variable}} in its own colour. The textarea on top is
// transparent and holds the caret, so the text is typed and selected like any
// other; the drawing underneath follows it as it scrolls.
//
// A fault is shown where it is: its line is marked in the gutter, on the line
// itself, and under the editor in words — the colour never says it alone.
export function JsonEditor({
  value,
  onChange,
  problem,
  readOnly = false,
  suggest,
  label = "Body",
  id,
  describedBy,
}: {
  value: string;
  onChange?: (value: string) => void;
  problem?: JsonProblem;
  readOnly?: boolean;
  suggest?: Suggesting;
  label?: string;
  id?: string;
  describedBy?: string;
}) {
  const [scroll, setScroll] = useState({ x: 0, y: 0 });
  const lines = value.split("\n");
  const fault = problem?.line;
  const area = useRef<HTMLTextAreaElement | null>(null);

  const { props, list } = useSuggestions({ value, onText: (text) => onChange?.(text), suggest });

  return (
    <div className="flex flex-col gap-1">
      <div
        className={cn(
          "relative flex h-48 overflow-hidden rounded-md border bg-background focus-within:ring-2 focus-within:ring-ring",
          problem && "border-foreground/40",
        )}
      >
        <div
          aria-hidden
          className="data w-9 shrink-0 overflow-hidden border-r bg-muted/30 py-2 text-right text-xs leading-5 text-muted-foreground select-none"
        >
          <div style={{ transform: `translateY(${-scroll.y}px)` }}>
            {lines.map((_, index) => (
              <div key={index} className={cn("px-1.5", fault === index + 1 && "bg-muted font-medium text-foreground")}>
                {index + 1}
              </div>
            ))}
          </div>
        </div>

        <div className="relative min-w-0 flex-1">
          <pre
            aria-hidden
            className="data pointer-events-none absolute inset-0 m-0 overflow-hidden py-2 pr-3 pl-2 text-xs leading-5"
          >
            <div style={{ transform: `translate(${-scroll.x}px, ${-scroll.y}px)` }}>
              {lines.map((line, index) => (
                <div key={index} className={cn("min-h-5 whitespace-pre", fault === index + 1 && "bg-muted")}>
                  {fault === index + 1 && problem ? (
                    <Marked line={line} column={problem.column} />
                  ) : (
                    tokenizeJsonLine(line).map((token, at) => (
                      <span key={at} className={kindClass[token.kind]}>
                        {token.text}
                      </span>
                    ))
                  )}
                </div>
              ))}
            </div>
          </pre>

          <textarea
            {...props}
            ref={(element) => {
              area.current = element;
              props.ref(element);
            }}
            id={id}
            aria-label={label}
            aria-invalid={problem ? true : undefined}
            aria-describedby={describedBy}
            readOnly={readOnly}
            spellCheck={false}
            autoComplete="off"
            wrap="off"
            value={value}
            onScroll={(event) => setScroll({ x: event.currentTarget.scrollLeft, y: event.currentTarget.scrollTop })}
            className="data absolute inset-0 h-full w-full resize-none overflow-auto bg-transparent py-2 pr-3 pl-2 text-xs leading-5 whitespace-pre text-transparent caret-foreground outline-none"
          />
          {list}
        </div>
      </div>

      {problem && (
        <p id={describedBy} role="alert" className="text-xs text-muted-foreground">
          <span className="data">
            Line {problem.line}, column {problem.column}
          </span>
          : {problem.message}
        </p>
      )}
    </div>
  );
}

// Marked draws the line a fault is on in one plain face, with the character it is
// at underlined, so that the line and the place are both visible without a hue.
function Marked({ line, column }: { line: string; column: number }) {
  const at = Math.max(0, Math.min(column - 1, line.length));
  return (
    <>
      <span>{line.slice(0, at)}</span>
      <span className="font-medium underline decoration-wavy underline-offset-4">{line.slice(at, at + 1) || " "}</span>
      <span>{line.slice(at + 1)}</span>
    </>
  );
}
