import { MailPlus, Undo2 } from "lucide-react";

import { ConfirmAction } from "@/components/confirm-action";
import { formatRelative } from "@/lib/format";

import { revokeInvitation } from "../actions";
import type { Invitation } from "../types";

// PendingInvitations are the invitations that have been neither used nor
// revoked. An expired one is listed, in words — "expired" — so that it can be
// withdrawn or replaced; it is not coloured, since colour is for runs.
//
// The link is not here and cannot be: it was shown once, when the invitation was
// created, and only its hash is kept. To hand one over again, revoke and invite.
export function PendingInvitations({
  projectId,
  invitations,
  now,
}: {
  projectId: string;
  invitations: Invitation[];
  now: Date;
}) {
  if (invitations.length === 0) return null;

  return (
    <section aria-labelledby="pending-title" className="flex flex-col gap-3">
      <h2 id="pending-title" className="text-[11px] tracking-wider text-muted-foreground uppercase">
        Pending invitations
      </h2>

      <ul className="flex flex-col divide-y rounded-xl border bg-card">
        {invitations.map((invitation) => (
          <li key={invitation.id} className="flex flex-wrap items-center gap-x-4 gap-y-2 px-4 py-3 text-sm">
            <MailPlus aria-hidden className="size-4 shrink-0 text-muted-foreground" />
            <span className="data min-w-0 flex-1 truncate text-[12.5px]">{invitation.email}</span>
            <span className="data text-xs text-muted-foreground">{invitation.role}</span>
            <span className="data text-xs text-muted-foreground">
              {invitation.state === "expired"
                ? `expired ${formatRelative(invitation.expires_at, now)}`
                : `expires ${formatRelative(invitation.expires_at, now)}`}
            </span>
            <ConfirmAction
              icon={<Undo2 aria-hidden />}
              label="Revoke"
              variant="outline"
              question={
                <>
                  Revoke the invitation of <span className="data">{invitation.email}</span>? The link stops working at
                  once.
                </>
              }
              confirm="Revoke invitation"
              busy="Revoking…"
              act={revokeInvitation.bind(null, projectId, invitation.id)}
            />
          </li>
        ))}
      </ul>
    </section>
  );
}
