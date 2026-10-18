import { EmptyState } from "@/components/states";
import { formatRelative } from "@/lib/format";
import { Users } from "lucide-react";

import { displayName, initials, lastAdmins, rolesGrantableBy, roleLockedReason } from "../roles";
import type { Member, Role } from "../types";
import { MemberMenu } from "./member-menu";
import { RoleControl } from "./role-control";

const columns = "grid grid-cols-[minmax(0,1.2fr)_minmax(0,1.4fr)_11rem_10rem_2.5rem] items-center gap-3 px-4";

// MembersTable lists who belongs to the project, with what they may do and when
// they last did anything in it.
//
// canManage hides what the caller cannot use; the API refuses them anyway. A
// role that cannot be changed stays on screen as a disabled control that says
// why, rather than disappearing.
export function MembersTable({
  projectId,
  members,
  caller,
  now,
}: {
  projectId: string;
  members: Member[];
  caller: { canManage: boolean; role: Role };
  now: Date;
}) {
  if (members.length === 0) {
    return <EmptyState icon={Users} title="Nobody here yet" description="Invite somebody to give them a role." />;
  }

  const last = lastAdmins(members);
  const options = rolesGrantableBy(caller.role);

  // The box is relative so that the screen-reader-only texts inside, which are
  // absolutely positioned, stay inside its clipping: without a positioned
  // ancestor they escape it and widen the whole page.

  return (
    <div className="relative overflow-x-auto rounded-xl border bg-card">
      <div role="table" aria-label="Members" className="min-w-[48rem]">
        <div role="row" className={`${columns} border-b py-2 text-[11px] tracking-wider text-muted-foreground uppercase`}>
          <div role="columnheader">Member</div>
          <div role="columnheader">Email</div>
          <div role="columnheader">Role</div>
          <div role="columnheader">Last activity</div>
          <div role="columnheader">
            <span className="sr-only">Actions</span>
          </div>
        </div>

        {members.map((member) => {
          const name = displayName(member);
          const locked = roleLockedReason(member, caller, last);

          return (
            <div
              key={member.user_id}
              role="row"
              className={`${columns} min-h-11 border-b py-1.5 text-sm last:border-b-0 hover:bg-muted/30`}
            >
              <div role="cell" className="flex min-w-0 items-center gap-2.5">
                <span
                  aria-hidden
                  className="grid size-6 shrink-0 place-items-center rounded-full border bg-secondary text-[10px]"
                >
                  {initials(member)}
                </span>
                <span className="truncate text-[12.5px]">{name}</span>
              </div>
              <div role="cell" className="data truncate text-[11.5px] text-muted-foreground">
                {member.email}
              </div>
              <div role="cell">
                <RoleControl
                  projectId={projectId}
                  userId={member.user_id}
                  name={name}
                  role={member.role}
                  options={options}
                  lockedBecause={locked}
                />
                {member.has_custom_permissions && (
                  <span className="text-[11px] text-muted-foreground">custom permissions</span>
                )}
              </div>
              <div role="cell" className="data text-[11.5px] text-muted-foreground">
                {member.last_activity ? formatRelative(member.last_activity, now) : "no activity yet"}
              </div>
              <div role="cell" className="flex justify-end">
                {caller.canManage && (
                  <MemberMenu
                    projectId={projectId}
                    userId={member.user_id}
                    name={name}
                    lockedBecause={last.has(member.user_id) ? "The project needs at least one admin." : null}
                  />
                )}
              </div>
            </div>
          );
        })}
      </div>
    </div>
  );
}
