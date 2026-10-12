import { redirect } from "next/navigation";

import { ApiError, apiGet, publicAPIURL } from "@/lib/api-client";

// The identity providers the API has credentials for.
export async function getProviders(): Promise<string[]> {
  const { providers } = await apiGet<{ providers: string[] }>("/auth/providers");
  return providers;
}

// Where the browser goes to sign in with provider. The API sends it on to the
// provider and, once signed in, back to next on this front end.
export function loginURL(provider: string, next: string): string {
  const url = new URL(`/auth/${encodeURIComponent(provider)}/login`, publicAPIURL);
  url.searchParams.set("next", next);
  return url.toString();
}

// Only a path on this front end; the API enforces the same rule.
export function safeNext(next: string | string[] | undefined): string {
  if (typeof next !== "string" || !next.startsWith("/") || next.startsWith("//") || next.includes("\\")) {
    return "/";
  }
  return next;
}

export type Me = {
  user: { id: string; name: string; email: string };
  organization: { id: string; name: string };
};

// getMe returns who is signed in. Without a session, the browser is sent to
// sign in, and brought back to returnTo afterwards.
export async function getMe(returnTo = "/"): Promise<Me> {
  try {
    return await apiGet<Me>("/me");
  } catch (error) {
    if (error instanceof ApiError && error.status === 401) {
      redirect(`/login?next=${encodeURIComponent(safeNext(returnTo))}`);
    }
    throw error;
  }
}
