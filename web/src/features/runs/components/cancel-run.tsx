"use client";

import { CircleStop } from "lucide-react";
import { useState, useTransition } from "react";

import { Button } from "@/components/ui/button";

import { cancelRun, type CancelState } from "../actions";

// CancelRun stops the run, after asking. The button is neutral, not red: red
// says a run failed. What it does is in its words, and it asks first.
export function CancelRun({ runId, running }: { runId: string; running: boolean }) {
  const [confirming, setConfirming] = useState(false);
  const [state, setState] = useState<CancelState>({});
  const [pending, startTransition] = useTransition();

  if (!confirming) {
    return (
      <Button variant="destructive" onClick={() => setConfirming(true)}>
        <CircleStop aria-hidden />
        Stop run
      </Button>
    );
  }

  return (
    <div role="alertdialog" aria-label="Stop this run?" className="flex flex-col gap-2 rounded-lg border bg-card p-3 text-sm">
      <p>
        {running
          ? "Stop this run? The load stops within a few seconds, and the run ends as cancelled."
          : "Cancel this run? It has not started; it will not."}
      </p>
      <div className="flex gap-2">
        <Button
          variant="destructive"
          disabled={pending}
          onClick={() =>
            startTransition(async () => {
              setState(await cancelRun(runId));
            })
          }
        >
          {pending ? "Stopping…" : running ? "Stop run" : "Cancel run"}
        </Button>
        <Button variant="ghost" disabled={pending} onClick={() => setConfirming(false)}>
          Keep it running
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
