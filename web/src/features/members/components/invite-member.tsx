"use client";

import { UserPlus, X } from "lucide-react";
import { useActionState, useRef, useState } from "react";

import { Button } from "@/components/ui/button";
import { CopyButton } from "@/features/environments/components/copy-button";

import { inviteMember, type InviteState } from "../actions";
import type { Role } from "../types";

const field =
  "h-9 rounded-md border bg-background px-3 text-sm outline-none focus-visible:ring-2 focus-visible:ring-ring";

// InviteMember is the "Invite member" button and its dialog.
//
// Nothing is sent: Ballast shows the link once and the administrator passes it
// on. That is said in the dialog, next to the link, because a link that was
// not copied cannot be shown again — the token is kept only as a hash.
export function InviteMember({ projectId, roles }: { projectId: string; roles: Role[] }) {
  const dialog = useRef<HTMLDialogElement>(null);
  // Bumped when the dialog is closed, so that the form starts again empty and
  // a link that was shown does not linger in memory for the next invitation.
  const [round, setRound] = useState(0);

  return (
    <>
      <Button
        onClick={() => {
          dialog.current?.showModal();
        }}
      >
        <UserPlus aria-hidden />
        Invite member
      </Button>

      <dialog
        ref={dialog}
        aria-labelledby="invite-title"
        onClose={() => setRound((n) => n + 1)}
        className="m-auto w-[min(28rem,calc(100vw-2rem))] rounded-xl border bg-card p-0 text-foreground backdrop:bg-background/70"
      >
        <InviteForm key={round} projectId={projectId} roles={roles} onDone={() => dialog.current?.close()} />
      </dialog>
    </>
  );
}

function InviteForm({ projectId, roles, onDone }: { projectId: string; roles: Role[]; onDone: () => void }) {
  const [state, action, pending] = useActionState<InviteState, FormData>(inviteMember.bind(null, projectId), {});

  return (
    <div className="flex flex-col gap-4 p-5">
      <div className="flex items-start justify-between gap-3">
        <h2 id="invite-title" className="text-lg font-normal tracking-tight">
          Invite a member
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

      {state.token ? (
        <InviteLink token={state.token} email={state.email ?? ""} onDone={onDone} />
      ) : state.added ? (
        <div className="flex flex-col gap-3 text-sm">
          <p>
            <span className="data">{state.email}</span> already has an account in this organization. They were added to
            the project; there is no link to send.
          </p>
          <Button onClick={onDone}>Done</Button>
        </div>
      ) : (
        <form action={action} className="flex flex-col gap-4">
          <label className="flex flex-col gap-1.5 text-sm">
            Email address
            <input
              autoFocus
              name="email"
              type="email"
              required
              autoComplete="off"
              placeholder="name@company.com"
              className={`${field} data`}
            />
          </label>

          <label className="flex flex-col gap-1.5 text-sm">
            Role
            <select name="role" defaultValue={roles.includes("runner") ? "runner" : roles[0]} className={field}>
              {roles.map((role) => (
                <option key={role} value={role}>
                  {role}
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
              {pending ? "Creating…" : "Create invitation"}
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

function InviteLink({ token, email, onDone }: { token: string; email: string; onDone: () => void }) {
  // The browser's own address: the link points back to this application.
  const link = `${window.location.origin}/invitations/${token}`;

  return (
    <div className="flex flex-col gap-3 text-sm">
      <p>
        Send this link to <span className="data">{email}</span>. It works once, for seven days, and only for someone
        signed in with that address.
      </p>
      <div className="flex items-center gap-2 rounded-md border bg-background p-2">
        <input
          readOnly
          aria-label="Invitation link"
          value={link}
          onFocus={(event) => event.currentTarget.select()}
          className="data min-w-0 flex-1 bg-transparent text-xs outline-none"
        />
        <CopyButton value={link} label="invitation link" />
      </div>
      <p className="text-xs text-muted-foreground">
        This is the only time it is shown: Ballast keeps no copy of it. To send another, revoke this invitation and
        invite again.
      </p>
      <Button onClick={onDone}>Done</Button>
    </div>
  );
}
