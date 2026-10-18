import { cache } from "react";

import { ApiError, apiGet } from "@/lib/api-client";

import type { Invitation, InvitationPreview, Member, RolesMatrix } from "./types";

function base(projectId: string): string {
  return `/projects/${encodeURIComponent(projectId)}`;
}

export async function getMembers(projectId: string): Promise<Member[]> {
  const { members } = await apiGet<{ members: Member[] }>(`${base(projectId)}/members`);
  return members;
}

// getInvitations are the open invitations of a project. Only an admin may read
// them; the page asks only when the caller is one.
export async function getInvitations(projectId: string): Promise<Invitation[]> {
  const { invitations } = await apiGet<{ invitations: Invitation[] }>(`${base(projectId)}/invitations`);
  return invitations;
}

export const getRoles = cache(() => apiGet<RolesMatrix>("/roles"));

// findInvitation reads what a link offers, and is null for a token nobody
// issued. It needs no session, but is called with one: the cookie is forwarded
// when there is one and harmless when there is not.
export async function findInvitation(token: string): Promise<InvitationPreview | null> {
  try {
    return await apiGet<InvitationPreview>(`/invitations/${encodeURIComponent(token)}`);
  } catch (error) {
    if (error instanceof ApiError && error.status === 404) return null;
    throw error;
  }
}
