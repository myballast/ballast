"use client";

import { Ellipsis, UserMinus, X } from "lucide-react";
import { useRef } from "react";

import { ConfirmAction } from "@/components/confirm-action";

import { removeMember } from "../actions";

// MemberMenu is the "…" of a row. It holds what can be done to a member other
// than changing their role, which today is taking them out of the project, and
// asks before doing it.
//
// The actions are in a modal <dialog>, not a menu hanging off the button: the
// table scrolls sideways on a narrow screen, and anything positioned inside a
// scrolling box is cut off by it. A dialog is drawn in the top layer, outside
// every box, and gives keyboard focus and Escape for free.
export function MemberMenu({
  projectId,
  userId,
  name,
  lockedBecause,
}: {
  projectId: string;
  userId: string;
  name: string;
  lockedBecause: string | null;
}) {
  const dialog = useRef<HTMLDialogElement>(null);

  return (
    <>
      <button
        type="button"
        aria-label={`More actions for ${name}`}
        aria-haspopup="dialog"
        onClick={() => dialog.current?.showModal()}
        className="grid size-7 place-items-center rounded-md text-muted-foreground hover:bg-muted hover:text-foreground"
      >
        <Ellipsis aria-hidden className="size-4" />
      </button>

      <dialog
        ref={dialog}
        aria-label={`Actions for ${name}`}
        className="m-auto w-[min(24rem,calc(100vw-2rem))] rounded-xl border bg-card p-0 text-foreground backdrop:bg-background/70"
      >
        <div className="flex flex-col gap-4 p-5">
          <div className="flex items-start justify-between gap-3">
            <h2 className="text-lg font-normal tracking-tight">{name}</h2>
            <button
              type="button"
              onClick={() => dialog.current?.close()}
              className="grid size-7 place-items-center rounded-md text-muted-foreground hover:bg-muted hover:text-foreground"
            >
              <X aria-hidden className="size-4" />
              <span className="sr-only">Close</span>
            </button>
          </div>

          {lockedBecause ? (
            <p className="text-sm text-muted-foreground">{lockedBecause}</p>
          ) : (
            <ConfirmAction
              icon={<UserMinus aria-hidden />}
              label="Remove from project"
              variant="outline"
              question={
                <>
                  Remove <span className="font-medium">{name}</span> from this project? They lose access at once and
                  can be invited again.
                </>
              }
              confirm="Remove member"
              busy="Removing…"
              act={removeMember.bind(null, projectId, userId)}
            />
          )}
        </div>
      </dialog>
    </>
  );
}
