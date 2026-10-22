"use client";

import { FileUp, ListPlus } from "lucide-react";
import Link from "next/link";
import { useRef, useState } from "react";

import { Button } from "@/components/ui/button";
import { EndpointPicker } from "@/features/endpoints/components/endpoint-picker";
import { insertAt, requestSnippet } from "@/features/endpoints/snippet";
import type { CatalogEndpoint } from "@/features/endpoints/types";
import { cn } from "@/lib/utils";

// ScriptEditor is a plain textarea in the monospace face, with line numbers in
// a gutter that scrolls with it. Lines are not wrapped, so that each number
// stays level with its line. The lines a finding points at are marked in the
// gutter — with weight and a background, never a status colour.
//
// A .js file from the disk can replace the text. It is read here, in the
// browser; it reaches the API only when the form is saved, like pasted text.
//
// With the project's endpoints, a request can be inserted at the cursor, from a
// button or with Ctrl+Space.
export function ScriptEditor({
  value,
  onChange,
  marked,
  readOnly = false,
  invalid = false,
  endpoints,
  importHref,
}: {
  value: string;
  onChange?: (value: string) => void;
  marked?: Set<number>;
  readOnly?: boolean;
  invalid?: boolean;
  // The project's catalog. Left out, the editor offers nothing to insert; empty,
  // it says where to import some.
  endpoints?: CatalogEndpoint[];
  importHref?: string;
}) {
  const gutter = useRef<HTMLDivElement>(null);
  const file = useRef<HTMLInputElement>(null);
  const textarea = useRef<HTMLTextAreaElement>(null);
  const [picking, setPicking] = useState(false);
  const count = Math.max(1, value.split("\n").length);
  const canInsert = !readOnly && endpoints !== undefined && endpoints.length > 0;

  // insert writes the request where the cursor was, and puts the cursor after
  // it. The selection is read now, before the picker took the focus away.
  function insert(endpoint: CatalogEndpoint) {
    const area = textarea.current;
    const start = area?.selectionStart ?? value.length;
    const end = area?.selectionEnd ?? value.length;

    const result = insertAt(value, start, end, requestSnippet(endpoint));
    onChange?.(result.text);
    setPicking(false);

    // The textarea shows the new text only after this render.
    setTimeout(() => {
      area?.focus();
      area?.setSelectionRange(result.cursor, result.cursor);
    }, 0);
  }

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
          ref={textarea}
          name="script"
          value={value}
          onChange={(event) => onChange?.(event.target.value)}
          onKeyDown={(event) => {
            if (canInsert && event.ctrlKey && event.code === "Space") {
              event.preventDefault();
              setPicking(true);
            }
          }}
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

      {picking && endpoints && (
        <EndpointPicker endpoints={endpoints} onPick={insert} onClose={() => setPicking(false)} />
      )}

      {!readOnly && (
        <div className="flex flex-wrap items-center gap-2">
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
          {canInsert && (
            <Button type="button" variant="outline" size="sm" onClick={() => setPicking(true)}>
              <ListPlus aria-hidden />
              Insert endpoint
              <span className="data text-xs text-muted-foreground">Ctrl+Space</span>
            </Button>
          )}
          {endpoints !== undefined && endpoints.length === 0 && importHref && (
            <span className="text-xs text-muted-foreground">
              No endpoint to insert yet.{" "}
              <Link href={importHref} className="underline">
                Import a specification
              </Link>
            </span>
          )}
        </div>
      )}
    </div>
  );
}
