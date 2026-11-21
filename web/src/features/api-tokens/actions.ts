"use server";

import { refresh, revalidatePath } from "next/cache";
import { redirect } from "next/navigation";

import { ApiError, apiDelete, apiPost } from "@/lib/api-client";

import { defaultExpiryDays, expiresAtFor, parseExpiryDays } from "./expiry";
import type { ApiToken } from "./types";

export type Outcome = { error?: string };

function tokensPage(projectId: string): string {
  return `/projects/${projectId}/tokens`;
}

// refusal turns what a token request can be refused for into a sentence. The
// API's own messages say exactly why and are written for whoever sent the
// request; none of them carries a secret.
function refusal(error: unknown, next: string): string {
  if (!(error instanceof ApiError)) throw error;
  switch (error.status) {
    case 401:
      redirect(`/login?next=${encodeURIComponent(next)}`);
    case 403:
    case 400:
    case 409:
    case 413:
    case 422:
      return error.message;
    case 404:
      return "That no longer exists. Reload the page.";
  }
  throw error;
}

// What creating tells back: an error, or the secret to copy. The secret comes
// back once, here, and is held by the form that asked for it; nothing can read it
// again, and this server keeps no copy.
export type CreateState = { error?: string; secret?: string; name?: string };

export async function createApiToken(projectId: string, _previous: CreateState, form: FormData): Promise<CreateState> {
  const name = String(form.get("name") ?? "").trim();
  if (!name) return { error: "A token needs a name, so that you can tell it from the others." };

  const days = parseExpiryDays(String(form.get("expires_in_days") ?? "")) ?? defaultExpiryDays;

  try {
    const created = await apiPost<{ token: ApiToken; secret: string }>(
      `/projects/${encodeURIComponent(projectId)}/api-tokens`,
      { name, expires_at: expiresAtFor(days, new Date()) },
    );

    revalidatePath(tokensPage(projectId));
    return { secret: created.secret, name: created.token.name };
  } catch (error) {
    return { error: refusal(error, tokensPage(projectId)) };
  }
}

export async function revokeApiToken(projectId: string, tokenId: string): Promise<Outcome> {
  try {
    await apiDelete(`/projects/${encodeURIComponent(projectId)}/api-tokens/${encodeURIComponent(tokenId)}`);
  } catch (error) {
    return { error: refusal(error, tokensPage(projectId)) };
  }

  refresh();
  return {};
}
