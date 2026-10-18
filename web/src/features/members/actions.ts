"use server";

import { refresh, revalidatePath } from "next/cache";
import { redirect } from "next/navigation";

import { ApiError, apiDelete, apiPost, apiPostAndAdoptSession, apiPut } from "@/lib/api-client";

import type { Invitation, Role } from "./types";

export type Outcome = { error?: string };

function toLogin(next: string): never {
  redirect(`/login?next=${encodeURIComponent(next)}`);
}

// refusal turns what a members request can be refused for into a sentence. The
// API's own messages for 400, 409 and 410 say exactly why and are written for
// whoever sent the request; none of them carries a token or an address that
// was not just typed.
function refusal(error: unknown, next: string): string {
  if (!(error instanceof ApiError)) throw error;
  switch (error.status) {
    case 401:
      toLogin(next);
    case 403:
      return error.message;
    case 404:
      return "That no longer exists. Reload the page.";
    case 400:
    case 409:
    case 410:
      return error.message;
  }
  throw error;
}

function members(projectId: string): string {
  return `/projects/${projectId}/members`;
}

// What inviting tells back: an error, the link to hand over, or — when the
// address already had an account in the organization — that the person was
// added and there is no link at all.
export type InviteState = { error?: string; token?: string; email?: string; added?: boolean };

// inviteMember writes an invitation. The token comes back once, here, and is
// held by the form that asked for it; nothing can read it again.
export async function inviteMember(projectId: string, _previous: InviteState, form: FormData): Promise<InviteState> {
  const email = String(form.get("email") ?? "").trim();
  const role = String(form.get("role") ?? "");
  if (!email) return { error: "An invitation needs an email address." };

  try {
    const result = await apiPost<{ token?: string; invitation?: Invitation; user_id?: string }>(
      `/projects/${encodeURIComponent(projectId)}/invitations`,
      { email, role },
    );

    revalidatePath(members(projectId));
    return result.token ? { token: result.token, email } : { added: true, email };
  } catch (error) {
    return { error: refusal(error, members(projectId)) };
  }
}

export async function revokeInvitation(projectId: string, invitationId: string): Promise<Outcome> {
  try {
    await apiDelete(`/projects/${encodeURIComponent(projectId)}/invitations/${encodeURIComponent(invitationId)}`);
  } catch (error) {
    return { error: refusal(error, members(projectId)) };
  }

  refresh();
  return {};
}

export async function changeRole(projectId: string, userId: string, role: Role): Promise<Outcome> {
  try {
    await apiPut(`/projects/${encodeURIComponent(projectId)}/members/${encodeURIComponent(userId)}`, { role });
  } catch (error) {
    return { error: refusal(error, members(projectId)) };
  }

  refresh();
  return {};
}

export async function removeMember(projectId: string, userId: string): Promise<Outcome> {
  try {
    await apiDelete(`/projects/${encodeURIComponent(projectId)}/members/${encodeURIComponent(userId)}`);
  } catch (error) {
    return { error: refusal(error, members(projectId)) };
  }

  refresh();
  return {};
}

// acceptInvitation follows a link. The answer is a session for the
// organization that invited, adopted here; the browser is then taken into the
// project it was invited to.
export async function acceptInvitation(token: string): Promise<Outcome> {
  const page = `/invitations/${token}`;

  let projectId: string;
  try {
    ({ project_id: projectId } = await apiPostAndAdoptSession<{ project_id: string }>(
      `/invitations/${encodeURIComponent(token)}/accept`,
      undefined,
    ));
  } catch (error) {
    return { error: refusal(error, page) };
  }

  // Everything drawn so far was drawn for the other organization.
  revalidatePath("/", "layout");
  redirect(`/projects/${projectId}`);
}

// switchOrganization acts for another organization the person belongs to.
export async function switchOrganization(organizationId: string): Promise<void> {
  try {
    await apiPostAndAdoptSession("/me/organization", { organization_id: organizationId });
  } catch (error) {
    refusal(error, "/projects");
    throw error;
  }

  revalidatePath("/", "layout");
  redirect("/projects");
}
