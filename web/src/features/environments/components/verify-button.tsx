"use client";

import { RefreshCw } from "lucide-react";
import { useState, useTransition } from "react";

import { Button } from "@/components/ui/button";

import { verifyEnvironment, type FormState } from "../actions";

// VerifyButton asks Ballast to fetch the challenge from the target. When the
// file is not there, the API says why, and that is shown as it is: it is the
// one thing that tells whoever runs the server what to fix.
export function VerifyButton({ projectId, environmentId, again }: { projectId: string; environmentId: string; again: boolean }) {
  const [state, setState] = useState<FormState>({});
  const [pending, startTransition] = useTransition();

  return (
    <div className="flex flex-col gap-2">
      <div>
        <Button
          disabled={pending}
          onClick={() =>
            startTransition(async () => {
              setState(await verifyEnvironment(projectId, environmentId));
            })
          }
        >
          <RefreshCw aria-hidden className={pending ? "animate-spin" : undefined} />
          {pending ? "Checking the target…" : again ? "Verify again" : "Verify"}
        </Button>
      </div>
      {state.error && (
        <p role="alert" className="text-sm text-muted-foreground">
          {state.error}
        </p>
      )}
    </div>
  );
}
