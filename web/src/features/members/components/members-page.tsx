import Link from "next/link";

import type { Project } from "@/features/projects/types";
import { can, type Access } from "@/lib/permissions";

import { rolesGrantableBy } from "../roles";
import type { Invitation, Member, RolesMatrix } from "../types";
import { InviteMember } from "./invite-member";
import { MembersTable } from "./members-table";
import { PendingInvitations } from "./pending-invitations";
import { PermissionsMatrix } from "./permissions-matrix";

// MembersPage is who has access to a project, what each role can do, and — for
// an admin — who has been invited and has not answered.
//
// What the caller cannot use is hidden or disabled here; the API refuses it
// whatever the page shows.
export function MembersPage({
  project,
  access,
  members,
  matrix,
  invitations,
  now,
}: {
  project: Project;
  access: Access;
  members: Member[];
  matrix: RolesMatrix;
  // Null for a caller who may not read them.
  invitations: Invitation[] | null;
  now: Date;
}) {
  const canManage = can(access, "manage_members");

  return (
    <div className="flex flex-col gap-8">
      <header className="flex flex-wrap items-start justify-between gap-4">
        <div className="flex flex-col gap-1">
          <Link href={`/projects/${project.id}`} className="text-xs text-muted-foreground hover:underline">
            {project.name}
          </Link>
          <h1 className="text-3xl font-normal tracking-tight">Members</h1>
          <p className="text-sm text-muted-foreground">
            <span className="num">{members.length}</span> {members.length === 1 ? "person has" : "people have"} access
            to {project.name}
          </p>
        </div>
        {canManage && <InviteMember projectId={project.id} roles={rolesGrantableBy(access.role)} />}
      </header>

      <PermissionsMatrix matrix={matrix} />
      <MembersTable projectId={project.id} members={members} caller={{ canManage, role: access.role }} now={now} />
      {invitations && <PendingInvitations projectId={project.id} invitations={invitations} now={now} />}
    </div>
  );
}
