"use client";

import { X } from "lucide-react";
import { useActionState, useRef, useState } from "react";

import { Button } from "@/components/ui/button";
import { CopyButton } from "@/features/environments/components/copy-button";

import type { SecretState } from "../actions";

// SecretDialog is a button and the dialog it opens, for the two things that end in a
// secret: making the webhook, and replacing its secret.
//
// The secret is shown once, here, and Ballast keeps it sealed, where nothing can read it
// back: a secret that was not copied is replaced, and the old one stops working. That is
// said next to the secret, where it is read.
export function SecretDialog({
  label,
  icon,
  variant = "default",
  title,
  intro,
  submit,
  busy,
  act,
}: {
  label: string;
  icon: React.ReactNode;
  variant?: "default" | "outline";
  title: string;
  intro: React.ReactNode;
  submit: string;
  busy: string;
  act: () => Promise<SecretState>;
}) {
  const dialog = useRef<HTMLDialogElement>(null);
  // Bumped when the dialog is closed, so that the form starts again empty and a secret
  // that was shown does not linger in memory.
  const [round, setRound] = useState(0);

  return (
    <>
      <Button variant={variant} onClick={() => dialog.current?.showModal()}>
        {icon}
        {label}
      </Button>

      <dialog
        ref={dialog}
        aria-labelledby="webhook-dialog-title"
        onClose={() => setRound((n) => n + 1)}
        className="m-auto w-[min(30rem,calc(100vw-2rem))] rounded-xl border bg-card p-0 text-foreground backdrop:bg-background/70"
      >
        <SecretForm
          key={round}
          title={title}
          intro={intro}
          submit={submit}
          busy={busy}
          act={act}
          onDone={() => dialog.current?.close()}
        />
      </dialog>
    </>
  );
}

function SecretForm({
  title,
  intro,
  submit,
  busy,
  act,
  onDone,
}: {
  title: string;
  intro: React.ReactNode;
  submit: string;
  busy: string;
  act: () => Promise<SecretState>;
  onDone: () => void;
}) {
  const [state, action, pending] = useActionState<SecretState, FormData>(() => act(), {});

  return (
    <div className="flex flex-col gap-4 p-5">
      <div className="flex items-start justify-between gap-3">
        <h2 id="webhook-dialog-title" className="text-lg font-normal tracking-tight">
          {state.secret ? "Your signing secret" : title}
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
        <RevealedWebhookSecret secret={state.secret} onDone={onDone} />
      ) : (
        <form action={action} className="flex flex-col gap-4">
          <div className="text-sm text-muted-foreground">{intro}</div>

          {state.error && (
            <p role="alert" className="text-sm text-muted-foreground">
              {state.error}
            </p>
          )}

          <div className="flex gap-2">
            <Button type="submit" disabled={pending}>
              {pending ? busy : submit}
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

// RevealedWebhookSecret is the state after the secret was made: the secret, once.
// Exported so that the reference page can show it without making one.
export function RevealedWebhookSecret({ secret, onDone }: { secret: string; onDone?: () => void }) {
  return (
    <div className="flex flex-col gap-3 text-sm">
      <p>
        Copy the secret now. Ballast shows it once and keeps it sealed, where nothing can read it back: if it is lost, replace it.
      </p>
      <div className="flex items-center gap-2 rounded-md border bg-background p-2">
        <input
          readOnly
          aria-label="Webhook signing secret"
          value={secret}
          onFocus={(event) => event.currentTarget.select()}
          className="data min-w-0 flex-1 bg-transparent text-xs outline-none"
        />
        <CopyButton value={secret} label="secret" />
      </div>
      <p className="text-xs text-muted-foreground">
        Keep it where the tool that signs the requests keeps its secrets. Never put it in a file, an argument or the
        repository.
      </p>
      {onDone && <Button onClick={onDone}>Done</Button>}
    </div>
  );
}
