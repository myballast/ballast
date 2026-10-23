"use client";

import { KeyRound } from "lucide-react";
import { useId, useRef, useState } from "react";

import { cn } from "@/lib/utils";

import { applySuggestion, suggestionContext, type Available } from "../variables";

// What typing {{ offers: a variable that is defined before this step, or a secret
// of the environment that no step reads yet. A variable bound to a secret is
// shown by its name and a mark: its value is not in the graph, so it is nowhere to
// be shown.
type Choice = { kind: "variable"; name: string; secret: boolean } | { kind: "bind"; key: string };

export type Suggesting = {
  available: Available[];
  // Keys of the environment's secrets that no step reads yet.
  unbound: string[];
  // Reads a secret into a new step before this request, then writes {{variable}}
  // into the field: the text is computed once the variable has its name.
  bind: (key: string, textFor: (variable: string) => string) => void;
};

type Field = HTMLInputElement | HTMLTextAreaElement;

// useSuggestions gives a text field the list that opens after {{. It returns what
// to spread on the field, and the list to render beside it. The field stays the
// caller's: a single-line input and the JSON editor's textarea use the same one.
export function useSuggestions({
  value,
  onText,
  suggest,
}: {
  value: string;
  onText: (text: string) => void;
  suggest?: Suggesting;
}) {
  const field = useRef<Field | null>(null);
  const id = useId();
  const [caret, setCaret] = useState<number | null>(null);
  const [index, setIndex] = useState(0);
  // Where the list was closed with Escape, so it stays closed until somebody types another {{.
  const [closedAt, setClosedAt] = useState<number | null>(null);

  const context = suggest && caret !== null ? suggestionContext(value, caret) : null;
  const query = context?.query.toLowerCase() ?? "";

  const choices: Choice[] =
    context && suggest
      ? [
          ...suggest.available
            .filter((v) => v.name.toLowerCase().includes(query))
            .map((v): Choice => ({ kind: "variable", name: v.name, secret: v.secret })),
          ...suggest.unbound.filter((key) => key.toLowerCase().includes(query)).map((key): Choice => ({ kind: "bind", key })),
        ]
      : [];

  const open = context !== null && choices.length > 0 && closedAt !== context.from;
  const active = Math.min(index, Math.max(0, choices.length - 1));

  function remember(element: Field) {
    setCaret(element.selectionStart);
    setIndex(0);
  }

  function put(text: string, at: number) {
    onText(text);
    setCaret(at);
    // The field shows the new text only after this render.
    requestAnimationFrame(() => {
      field.current?.focus();
      field.current?.setSelectionRange(at, at);
    });
  }

  function pick(choice: Choice) {
    if (!context || caret === null) return;

    if (choice.kind === "variable") {
      const done = applySuggestion(value, caret, context, choice.name);
      put(done.text, done.caret);
      return;
    }

    suggest?.bind(choice.key, (variable) => applySuggestion(value, caret, context, variable).text);
  }

  const props = {
    ref: (element: Field | null) => {
      field.current = element;
    },
    onChange: (event: React.ChangeEvent<Field>) => {
      onText(event.target.value);
      remember(event.target);
      setClosedAt(null);
    },
    onKeyUp: (event: React.KeyboardEvent<Field>) => {
      // Moving the caret with the arrows changes what it is inside of.
      if (!["ArrowDown", "ArrowUp", "Enter", "Tab", "Escape"].includes(event.key)) remember(event.currentTarget);
    },
    onClick: (event: React.MouseEvent<Field>) => remember(event.currentTarget),
    onBlur: () => setCaret(null),
    onKeyDown: (event: React.KeyboardEvent<Field>) => {
      if (!open) return;
      if (event.key === "ArrowDown" || event.key === "ArrowUp") {
        event.preventDefault();
        setIndex((active + (event.key === "ArrowDown" ? 1 : -1) + choices.length) % choices.length);
      } else if (event.key === "Enter" || event.key === "Tab") {
        event.preventDefault();
        pick(choices[active]);
      } else if (event.key === "Escape") {
        event.preventDefault();
        event.stopPropagation();
        setClosedAt(context?.from ?? null);
      }
    },
    role: "combobox" as const,
    "aria-expanded": open,
    "aria-controls": open ? `${id}-list` : undefined,
    "aria-activedescendant": open ? `${id}-${active}` : undefined,
    "aria-autocomplete": "list" as const,
  };

  const list = open ? (
    <ul
      id={`${id}-list`}
      role="listbox"
      aria-label="Variables"
      className="absolute top-full left-0 z-20 mt-1 max-h-48 w-full min-w-48 overflow-auto rounded-lg border bg-card p-1 text-xs"
    >
      {choices.map((choice, position) => (
        <li
          key={choice.kind === "variable" ? `v:${choice.name}` : `b:${choice.key}`}
          id={`${id}-${position}`}
          role="option"
          aria-selected={position === active}
          // The field keeps the focus: a click on the list must not blur it first.
          onMouseDown={(event) => event.preventDefault()}
          onClick={() => pick(choice)}
          className={cn(
            "data flex cursor-pointer items-center gap-2 rounded-md px-2 py-1.5",
            position === active ? "bg-muted text-foreground" : "text-muted-foreground",
          )}
        >
          {choice.kind === "variable" ? (
            <>
              <span className="truncate">{choice.name}</span>
              {choice.secret && (
                <span className="ml-auto flex shrink-0 items-center gap-1 rounded border px-1 text-[10px]">
                  <KeyRound aria-hidden className="size-3" />
                  secret
                </span>
              )}
            </>
          ) : (
            <>
              <KeyRound aria-hidden className="size-3 shrink-0" />
              <span className="truncate">Bind {choice.key}</span>
              <span className="ml-auto shrink-0 text-[10px]">adds a step</span>
            </>
          )}
        </li>
      ))}
    </ul>
  ) : null;

  return { props, list };
}
