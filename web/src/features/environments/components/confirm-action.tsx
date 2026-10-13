"use client";

import { useState, useTransition } from "react";

import { Button } from "@/components/ui/button";

import type { FormState } from "../actions";

// ConfirmAction is a button that asks before it acts: approving a target,
// deleting an environment or a secret. What it does is in its words; nothing
// here is red, since red says a run failed.
export function ConfirmAction({
  icon,
  label,
  question,
  confirm,
  keep = "Cancel",
  busy,
  variant = "default",
  act,
}: {
  // An element rather than a component: this is a client component, and a
  // server component can hand it rendered output but not a function.
  icon: React.ReactNode;
  label: string;
  question: React.ReactNode;
  confirm: string;
  keep?: string;
  busy: string;
  variant?: "default" | "outline" | "destructive" | "ghost";
  act: () => Promise<FormState>;
}) {
  const [confirming, setConfirming] = useState(false);
  const [state, setState] = useState<FormState>({});
  const [pending, startTransition] = useTransition();

  if (!confirming) {
    return (
      <Button variant={variant} onClick={() => setConfirming(true)}>
        {icon}
        {label}
      </Button>
    );
  }

  return (
    <div role="alertdialog" aria-label={label} className="flex flex-col gap-2 rounded-lg border bg-card p-3 text-sm">
      <p>{question}</p>
      <div className="flex flex-wrap gap-2">
        <Button
          variant={variant}
          disabled={pending}
          onClick={() =>
            startTransition(async () => {
              const next = await act();
              setState(next);
              if (!next.error) setConfirming(false);
            })
          }
        >
          {pending ? busy : confirm}
        </Button>
        <Button
          variant="ghost"
          disabled={pending}
          onClick={() => {
            setConfirming(false);
            setState({});
          }}
        >
          {keep}
        </Button>
      </div>
      {state.error && (
        <p role="alert" className="text-muted-foreground">
          {state.error}
        </p>
      )}
    </div>
  );
}
