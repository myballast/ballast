import { Time } from "@/components/time";

import type { InvitationPreview } from "../types";
import { AcceptInvitation } from "./accept-invitation";

// What each state of a link says. Said in words, never a colour: a link that
// has stopped working is not a failed run.
const unusable: Record<Exclude<InvitationPreview["state"], "pending">, string> = {
  accepted: "This invitation has already been used.",
  revoked: "This invitation was withdrawn by an admin of the project.",
  expired: "This invitation has expired. Ask an admin of the project for a new one.",
};

// InvitationCard is the page a link opens: who invited you, to what, as what —
// and, when the link still works, the one button that accepts.
//
// signedInAs is the address of the session, compared with the one the
// invitation was written for. Accepting with another address is refused by the
// API; saying so here saves the trip.
export function InvitationCard({
  token,
  invitation,
  signedInAs,
  now,
}: {
  token: string;
  invitation: InvitationPreview;
  signedInAs: string;
  now: Date;
}) {
  const sameAddress = signedInAs.trim().toLowerCase() === invitation.email;

  return (
    <div className="flex w-full max-w-md flex-col gap-6">
      <div className="flex flex-col gap-1.5">
        <p className="text-xs text-muted-foreground">{invitation.organization_name}</p>
        <h1 className="text-3xl font-normal tracking-tight">Join {invitation.project_name}</h1>
        <p className="text-sm text-muted-foreground">
          You have been invited as <span className="data">{invitation.role}</span>, with the address{" "}
          <span className="data">{invitation.email}</span>.
        </p>
      </div>

      {invitation.state !== "pending" ? (
        <p role="status" className="rounded-lg border bg-card px-3 py-2 text-sm text-muted-foreground">
          {unusable[invitation.state]}
        </p>
      ) : !sameAddress ? (
        <p role="status" className="rounded-lg border bg-card px-3 py-2 text-sm text-muted-foreground">
          You are signed in as <span className="data">{signedInAs}</span>. This invitation is for{" "}
          <span className="data">{invitation.email}</span>: sign in with that address to accept it.
        </p>
      ) : (
        <>
          <AcceptInvitation token={token} project={invitation.project_name} />
          <p className="text-xs text-muted-foreground">
            Expires <span className="num"><Time iso={invitation.expires_at} mode="relative" now={now} /></span>. You keep your own
            organization and can switch between them.
          </p>
        </>
      )}
    </div>
  );
}
