import type { Member, Role } from "./types";

// The four roles, least able first. The order is the API's (GET /roles lists
// them the same way) and is what "above your own" means.
export const roleOrder: Role[] = ["viewer", "runner", "maintainer", "admin"];

export function rank(role: Role): number {
  return roleOrder.indexOf(role) + 1;
}

// rolesGrantableBy are the roles somebody holding role may give: up to their
// own and no higher. The API refuses the rest; the interface does not offer it.
export function rolesGrantableBy(role: Role): Role[] {
  return roleOrder.filter((candidate) => rank(candidate) <= rank(role));
}

// The ids of the members who are the last administrator of the project. At
// most one: when there are two, nobody is the last.
export function lastAdmins(members: Member[]): Set<string> {
  const admins = members.filter((m) => m.role === "admin");
  return new Set(admins.length === 1 ? [admins[0].user_id] : []);
}

// Why a member's role cannot be changed by the caller, or null when it can. The
// sentence is the tooltip of the disabled control.
export function roleLockedReason(
  member: Member,
  caller: { canManage: boolean; role: Role },
  last: Set<string>,
): string | null {
  if (!caller.canManage) return "Only an admin can change roles.";
  if (last.has(member.user_id)) return "The project needs at least one admin.";
  if (rank(member.role) > rank(caller.role)) return "You cannot change the role of somebody above your own.";
  return null;
}

// initials are the two letters in the circle: the first of the first and last
// words of the name, or the first two letters of the address when there is no
// name.
export function initials(member: Pick<Member, "name" | "email">): string {
  const words = (member.name ?? "").split(/\s+/).filter(Boolean);
  if (words.length >= 2) return (words[0][0] + words[words.length - 1][0]).toUpperCase();
  if (words.length === 1) return words[0].slice(0, 2).toUpperCase();
  return member.email.slice(0, 2).toUpperCase();
}

export function displayName(member: Pick<Member, "name" | "email">): string {
  return member.name?.trim() || member.email.split("@")[0];
}
