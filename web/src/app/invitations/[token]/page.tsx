import { notFound } from "next/navigation";

import { getMe } from "@/features/auth/api";
import { findInvitation } from "@/features/members/api";
import { InvitationCard } from "@/features/members/components/invitation-card";

// A link is its own page, outside the application's frame: the person following
// it may belong to no project yet, and the frame is a project's.
export default async function InvitationRoute(props: PageProps<"/invitations/[token]">) {
  const { token } = await props.params;

  // Without a session the browser is sent to sign in, and brought back here.
  const me = await getMe(`/invitations/${token}`);
  const invitation = await findInvitation(token);
  if (!invitation) notFound();

  return (
    <main className="flex flex-1 items-center justify-center px-4 py-16">
      <InvitationCard token={token} invitation={invitation} signedInAs={me.user.email} now={new Date()} />
    </main>
  );
}
