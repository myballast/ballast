import { apiGet, publicAPIURL } from "@/lib/api-client";

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
