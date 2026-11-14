import { CircleAlert, TriangleAlert } from "lucide-react";

import { cn } from "@/lib/utils";

import type { LogLevel } from "../log-level";

// The level is written out, with an icon, on every line that has one: the
// colour is a help for the eye and never what says it.
const levels = {
  error: {
    word: "ERROR",
    icon: CircleAlert,
    row: "border-l-2 border-log-error bg-log-error-bg",
    text: "text-log-error-text",
  },
  warn: {
    word: "WARN",
    icon: TriangleAlert,
    row: "border-l-2 border-log-warn bg-log-warn-bg",
    text: "text-log-warn-text",
  },
} as const;

// LogLine is one line of a run's output: its number, when it was written, its
// level if it has one, and the text.
//
// This is the one place in Ballast where red and amber appear outside a status
// — the level of a line of output (an exception of the colour charter). They have
// tokens of their own, log-error and log-warn, and `npm run lint` keeps them
// out of every other file.
export function LogLine({
  seq,
  time,
  level,
  children,
}: {
  seq: number;
  time: string;
  level: LogLevel | null;
  children: React.ReactNode;
}) {
  const style = level ? levels[level] : null;
  const Icon = style?.icon;

  return (
    <div className={cn("flex gap-3 border-l-2 border-transparent px-3 whitespace-pre-wrap", style?.row)}>
      <span className="w-10 shrink-0 text-right text-muted-foreground select-none">{seq}</span>
      <span className="w-[4.5rem] shrink-0 text-muted-foreground select-none">{time}</span>
      {style && Icon ? (
        <span className={cn("flex w-[3.75rem] shrink-0 items-center gap-1", style.text)}>
          <Icon aria-hidden className="size-3 shrink-0" />
          {style.word}
        </span>
      ) : (
        <span aria-hidden className="w-[3.75rem] shrink-0" />
      )}
      <span className={cn("min-w-0", style?.text)}>{children}</span>
    </div>
  );
}
