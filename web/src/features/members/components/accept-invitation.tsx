"use client";

import { useState, useTransition } from "react";

import { Button } from "@/components/ui/button";

import { acceptInvitation } from "../actions";

// AcceptInvitation is the button of an invitation page. On success the action
// takes the browser into the project, so there is nothing to show but a failure.
export function AcceptInvitation({ token, project }: { token: string; project: string }) {
  const [error, setError] = useState<string | null>(null);
  const [pending, startTransition] = useTransition();

  return (
    <div className="flex flex-col gap-3">
      <Button
        size="lg"
        disabled={pending}
        onClick={() =>
          startTransition(async () => {
            const outcome = await acceptInvitation(token);
            if (outcome?.error) setError(outcome.error);
          })
        }
      >
        {pending ? "Joining…" : `Join ${project}`}
      </Button>
      {error && (
        <p role="alert" className="text-sm text-muted-foreground">
          {error}
        </p>
      )}
    </div>
  );
}
