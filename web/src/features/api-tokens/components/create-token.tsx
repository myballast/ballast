"use client";

import { KeyRound, X } from "lucide-react";
import { useActionState, useRef, useState } from "react";

import { Button } from "@/components/ui/button";
import { CopyButton } from "@/features/environments/components/copy-button";

import { createApiToken, type CreateState } from "../actions";
import { defaultExpiryDays, expiryOptions } from "../expiry";

const field =
  "h-9 rounded-md border bg-background px-3 text-sm outline-none focus-visible:ring-2 focus-visible:ring-ring";

// CreateToken is the "Create token" button and its dialog.
//
// The secret is shown once, here, and Ballast keeps only its hash: a token that
// was not copied cannot be shown again, and the way to replace it is to revoke it
// and make another. That is said next to the secret, where it is read.
//
// When the project is at its ceiling the button stays, disabled, and says why: a
// button that disappears leaves the reader looking for it.
export function CreateToken({
  projectId,
  full,
  limit,
}: {
  projectId: string;
  // Whether the project already holds the most tokens it may.
  full: boolean;
  limit: number;
}) {
  const dialog = useRef<HTMLDialogElement>(null);
  // Bumped when the dialog is closed, so that the form starts again empty and a
  // secret that was shown does not linger in memory for the next token.
  const [round, setRound] = useState(0);

  return (
    <>
      <div className="flex flex-col items-end gap-1">
        <Button
          disabled={full}
          aria-describedby={full ? "token-limit" : undefined}
          onClick={() => {
            dialog.current?.showModal();
          }}
        >
          <KeyRound aria-hidden />
          Create token
        </Button>
        {full && (
          <p id="token-limit" className="max-w-56 text-right text-xs text-muted-foreground">
            This project holds {limit} tokens, the most it may. Revoke one to make another.
          </p>
        )}
      </div>

      <dialog
        ref={dialog}
        aria-labelledby="token-title"
        onClose={() => setRound((n) => n + 1)}
        className="m-auto w-[min(30rem,calc(100vw-2rem))] rounded-xl border bg-card p-0 text-foreground backdrop:bg-background/70"
      >
        <TokenForm key={round} projectId={projectId} onDone={() => dialog.current?.close()} />
      </dialog>
    </>
  );
}

function TokenForm({ projectId, onDone }: { projectId: string; onDone: () => void }) {
  const [state, action, pending] = useActionState<CreateState, FormData>(createApiToken.bind(null, projectId), {});

  return (
    <div className="flex flex-col gap-4 p-5">
      <div className="flex items-start justify-between gap-3">
        <h2 id="token-title" className="text-lg font-normal tracking-tight">
          {state.secret ? "Your new token" : "Create a CI token"}
        </h2>
        <button
          type="button"
          onClick={onDone}
          className="grid size-7 place-items-center rounded-md text-muted-foreground hover:bg-muted hover:text-foreground"
        >
          <X aria-hidden className="size-4" />
          <span className="sr-only">Close</span>
        </button>
      </div>

      {state.secret ? (
        <RevealedSecret secret={state.secret} name={state.name ?? ""} onDone={onDone} />
      ) : (
        <form action={action} className="flex flex-col gap-4">
          <p className="text-sm text-muted-foreground">
            A token lets a pipeline read this project and launch runs in it. It can do nothing else.
          </p>

          <label className="flex flex-col gap-1.5 text-sm">
            Name
            <input
              autoFocus
              name="name"
              required
              maxLength={64}
              autoComplete="off"
              placeholder="github-main"
              className={`${field} data`}
            />
            <span className="text-xs text-muted-foreground">Says where it is used, so that you can revoke the right one.</span>
          </label>

          <label className="flex flex-col gap-1.5 text-sm">
            Expires
            <select name="expires_in_days" defaultValue={String(defaultExpiryDays)} className={field}>
              {expiryOptions.map((option) => (
                <option key={option.days} value={option.days}>
                  {option.label}
                  {option.days === defaultExpiryDays ? " (default)" : ""}
                </option>
              ))}
            </select>
          </label>

          {state.error && (
            <p role="alert" className="text-sm text-muted-foreground">
              {state.error}
            </p>
          )}

          <div className="flex gap-2">
            <Button type="submit" disabled={pending}>
              {pending ? "Creating…" : "Create token"}
            </Button>
            <Button type="button" variant="ghost" onClick={onDone}>
              Cancel
            </Button>
          </div>
        </form>
      )}
    </div>
  );
}

// RevealedSecret is the state after a token was created: the secret, once. Exported
// so that the reference page can show it without creating a token.
export function RevealedSecret({ secret, name, onDone }: { secret: string; name: string; onDone?: () => void }) {
  return (
    <div className="flex flex-col gap-3 text-sm">
      <p>
        Copy the token <span className="data">{name}</span> now. Ballast shows it once and keeps only a fingerprint of
        it: it cannot be shown again.
      </p>
      <div className="flex items-center gap-2 rounded-md border bg-background p-2">
        <input
          readOnly
          aria-label="API token"
          value={secret}
          onFocus={(event) => event.currentTarget.select()}
          className="data min-w-0 flex-1 bg-transparent text-xs outline-none"
        />
        <CopyButton value={secret} label="token" />
      </div>
      <p className="text-xs text-muted-foreground">
        In your pipeline, store it as a secret named <span className="data">BALLAST_TOKEN</span>. Never put it in a
        file, an argument or the repository. To replace it, revoke it and create another.
      </p>
      {onDone && <Button onClick={onDone}>Done</Button>}
    </div>
  );
}
