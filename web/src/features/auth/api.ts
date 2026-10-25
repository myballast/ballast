import { cache } from "react";
import { redirect } from "next/navigation";

import { ApiError, apiGet } from "@/lib/api-client";

import { safeNext } from "./login-url";

// The identity providers the API has credentials for.
export async function getProviders(): Promise<string[]> {
  const { providers } = await apiGet<{ providers: string[] }>("/auth/providers");
  return providers;
}

export type Me = {
  user: { id: string; name: string; email: string };
  organization: { id: string; name: string };
  // Every organization the person belongs to, the current one included.
  organizations: { id: string; name: string }[];
};

// getMe returns who is signed in. Without a session, the browser is sent to
// sign in, and brought back to returnTo afterwards.
//
// Cached for the request: every layout that draws the shell asks who is signed
// in, and a page rendered under two of them should not ask twice.
export const getMe = cache(async (returnTo = "/"): Promise<Me> => {
  try {
    return await apiGet<Me>("/me");
  } catch (error) {
    if (error instanceof ApiError && error.status === 401) {
      redirect(`/login?next=${encodeURIComponent(safeNext(returnTo))}`);
    }
    throw error;
  }
});
