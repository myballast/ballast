"use server";

import { refresh, revalidatePath } from "next/cache";
import { redirect } from "next/navigation";

import { ApiError, apiDelete, apiPost } from "@/lib/api-client";

import type { Webhook } from "./types";

export type Outcome = { error?: string };

// What making or replacing the secret tells back: an error, or the secret to copy. It
// comes back once, here, and is held by the form that asked for it; nothing can read it
// again, and this server keeps no copy.
export type SecretState = { error?: string; secret?: string };

function webhookPage(projectId: string): string {
  return `/projects/${projectId}/webhook`;
}

// refusal turns what a webhook request can be refused for into a sentence. The API's own
// messages say exactly why and are written for whoever sent the request; none of them
// carries a secret.
function refusal(error: unknown, next: string): string {
  if (!(error instanceof ApiError)) throw error;
  switch (error.status) {
    case 401:
      redirect(`/login?next=${encodeURIComponent(next)}`);
    case 403:
    case 409:
      return error.message;
    case 404:
      return "That no longer exists. Reload the page.";
  }
  throw error;
}

export async function createWebhook(projectId: string): Promise<SecretState> {
  return secretFrom(projectId, `/projects/${encodeURIComponent(projectId)}/webhook`);
}

export async function rotateWebhook(projectId: string): Promise<SecretState> {
  return secretFrom(projectId, `/projects/${encodeURIComponent(projectId)}/webhook/rotate`);
}

async function secretFrom(projectId: string, path: string): Promise<SecretState> {
  try {
    const made = await apiPost<{ webhook: Webhook; secret: string }>(path, {});
    revalidatePath(webhookPage(projectId));
    return { secret: made.secret };
  } catch (error) {
    return { error: refusal(error, webhookPage(projectId)) };
  }
}

export async function deleteWebhook(projectId: string): Promise<Outcome> {
  try {
    await apiDelete(`/projects/${encodeURIComponent(projectId)}/webhook`);
  } catch (error) {
    return { error: refusal(error, webhookPage(projectId)) };
  }

  refresh();
  return {};
}
