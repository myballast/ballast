"use client";

import { FileUp } from "lucide-react";
import { useRef } from "react";

import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";

// ScriptEditor is a plain textarea in the monospace face, with line numbers in
// a gutter that scrolls with it. Lines are not wrapped, so that each number
// stays level with its line. The lines a finding points at are marked in the
// gutter — with weight and a background, never a status colour.
//
// A .js file from the disk can replace the text. It is read here, in the
// browser; it reaches the API only when the form is saved, like pasted text.
export function ScriptEditor({
  value,
  onChange,
  marked,
  readOnly = false,
  invalid = false,
}: {
  value: string;
  onChange?: (value: string) => void;
  marked?: Set<number>;
  readOnly?: boolean;
  invalid?: boolean;
}) {
  const gutter = useRef<HTMLDivElement>(null);
  const file = useRef<HTMLInputElement>(null);
  const count = Math.max(1, value.split("\n").length);

  return (
    <div className="flex flex-col gap-2">
      <div
        className={cn(
          "flex max-h-[32rem] min-h-64 overflow-hidden rounded-md border bg-background focus-within:ring-2 focus-within:ring-ring",
          invalid && "border-foreground/60",
        )}
      >
        <div
          ref={gutter}
          aria-hidden
          className="data shrink-0 overflow-hidden border-r py-2 text-right text-xs leading-5 text-muted-foreground select-none"
        >
          {Array.from({ length: count }, (_, i) => i + 1).map((n) => (
            <div
              key={n}
              className={cn("px-2", marked?.has(n) && "bg-muted font-medium text-foreground")}
            >
              {n}
            </div>
          ))}
        </div>
        <textarea
          name="script"
          value={value}
          onChange={(event) => onChange?.(event.target.value)}
          onScroll={(event) => {
            if (gutter.current) gutter.current.scrollTop = event.currentTarget.scrollTop;
          }}
          readOnly={readOnly}
          required
          wrap="off"
          spellCheck={false}
          autoCapitalize="off"
          autoCorrect="off"
          aria-label="Script"
          aria-invalid={invalid || undefined}
          className="data min-w-0 flex-1 resize-none overflow-auto bg-transparent px-3 py-2 text-xs leading-5 outline-none"
        />
      </div>

      {!readOnly && (
        <div>
          <input
            ref={file}
            type="file"
            accept=".js,.mjs,text/javascript"
            className="sr-only"
            tabIndex={-1}
            onChange={async (event) => {
              const chosen = event.target.files?.[0];
              if (chosen) onChange?.(await chosen.text());
              event.target.value = "";
            }}
          />
          <Button type="button" variant="outline" size="sm" onClick={() => file.current?.click()}>
            <FileUp aria-hidden />
            Import a .js file
          </Button>
        </div>
      )}
    </div>
  );
}
