"use client";

import { KeyRound, Plus, Trash2 } from "lucide-react";
import { useActionState, useRef } from "react";

import { EmptyState } from "@/components/states";
import { Button } from "@/components/ui/button";
import { formatDateTime } from "@/lib/format";

import { deleteSecret, setSecret, type SecretState } from "../actions";
import type { Secret } from "../types";
import { ConfirmAction } from "./confirm-action";

const field =
  "h-9 rounded-md border bg-background px-3 text-sm outline-none focus-visible:ring-2 focus-visible:ring-ring";

// SecretList shows the keys of an environment's secrets and when they were
// set — never a value: the API returns none, and there is nowhere here one
// could be shown. A value is typed, sent, and the field is emptied.
//
// Setting a key that exists replaces its value, which is how a secret is
// rotated.
export function SecretList({
  projectId,
  environmentId,
  secrets,
}: {
  projectId: string;
  environmentId: string;
  secrets: Secret[];
}) {
  const form = useRef<HTMLFormElement>(null);
  const [state, action, pending] = useActionState<SecretState, FormData>(async (previous, data) => {
    const next = await setSecret(projectId, environmentId, previous, data);
    // The value leaves the page as soon as it has been sent, whatever the
    // answer. A refused key keeps its key, so it can be corrected.
    const value = form.current?.elements.namedItem("value");
    if (value instanceof HTMLInputElement) value.value = "";
    if (next.saved) form.current?.reset();
    return next;
  }, {});

  return (
    <div className="flex flex-col gap-4">
      {secrets.length === 0 ? (
        <EmptyState
          icon={KeyRound}
          title="No secret"
          description="Secrets reach the k6 container as environment variables, for this environment only. Their values are never shown again."
        />
      ) : (
        <ul className="flex flex-col divide-y rounded-xl border">
          {secrets.map((secret) => (
            <li key={secret.id} className="flex flex-wrap items-center justify-between gap-3 px-4 py-3 text-sm">
              <span className="flex min-w-0 flex-col gap-1">
                <span className="data break-all">{secret.key}</span>
                <span className="text-xs text-muted-foreground">
                  set <span className="num">{formatDateTime(secret.updated_at)}</span>
                </span>
              </span>
              <ConfirmAction
                icon={<Trash2 aria-hidden />}
                label="Delete"
                variant="destructive"
                question={
                  <>
                    Delete <span className="data">{secret.key}</span>? Runs started from now on will not have it.
                  </>
                }
                confirm="Delete secret"
                busy="Deleting…"
                act={deleteSecret.bind(null, projectId, environmentId, secret.key)}
              />
            </li>
          ))}
        </ul>
      )}

      <form ref={form} action={action} aria-label="Add a secret" className="flex flex-col gap-3 rounded-xl border p-4">
        <div className="grid gap-3 sm:grid-cols-2">
          <label className="flex flex-col gap-1.5 text-sm">
            Key
            <input
              name="key"
              required
              maxLength={128}
              pattern="[A-Z][A-Z0-9_]*"
              title="Uppercase letters, digits and underscores, starting with a letter"
              placeholder="API_TOKEN"
              autoComplete="off"
              spellCheck={false}
              className={`data ${field}`}
            />
          </label>
          <label className="flex flex-col gap-1.5 text-sm">
            Value
            <input
              name="value"
              type="password"
              required
              autoComplete="new-password"
              spellCheck={false}
              className={`data ${field}`}
            />
          </label>
        </div>
        <span className="text-xs text-muted-foreground">
          A key that already exists gets the new value. Keys starting with K6_ are refused: k6 would read them as its
          own settings.
        </span>
        {state.error && (
          <p role="alert" className="text-sm text-muted-foreground">
            {state.error}
          </p>
        )}
        {state.saved && !state.error && (
          <p role="status" className="text-sm text-muted-foreground">
            <span className="data">{state.saved}</span> saved.
          </p>
        )}
        <div>
          <Button type="submit" disabled={pending}>
            <Plus aria-hidden />
            {pending ? "Saving…" : "Save secret"}
          </Button>
        </div>
      </form>
    </div>
  );
}
