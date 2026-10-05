import { cookies } from "next/headers";

// The only way the front talks to the Go API.
//
// It runs on the server: the base URL is not prefixed with NEXT_PUBLIC_, so it
// never reaches the browser. The browser's session cookie is forwarded to the
// API on its behalf; page scripts never see it, since it is HttpOnly.
const baseURL = process.env.BALLAST_API_URL ?? "http://localhost:8080";

// Where the browser itself reaches the API. Only sign-in needs it: the
// browser has to be sent to the API's /auth/{provider}/login, which redirects
// it on to the identity provider.
export const publicAPIURL = process.env.BALLAST_API_PUBLIC_URL ?? baseURL;

// Must match session.CookieName in internal/adapter/http/session.
const sessionCookie = "ballast_session";

export class ApiError extends Error {
  constructor(
    readonly status: number,
    message: string,
  ) {
    super(message);
    this.name = "ApiError";
  }
}

export async function apiGet<T>(path: string): Promise<T> {
  const headers: Record<string, string> = { Accept: "application/json" };

  const session = (await cookies()).get(sessionCookie);
  if (session) {
    headers.Cookie = `${sessionCookie}=${session.value}`;
  }

  const response = await fetch(`${baseURL}${path}`, {
    cache: "no-store",
    headers,
  });

  if (!response.ok) {
    // Every failed response from the API carries {"error": "..."}.
    const body = (await response.json().catch(() => null)) as {
      error?: string;
    } | null;
    throw new ApiError(response.status, body?.error ?? response.statusText);
  }

  return (await response.json()) as T;
}
