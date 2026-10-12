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
export const sessionCookie = "ballast_session";

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

// apiStream opens a streaming response on the API, for a route handler to pass
// straight through to the browser.
//
// It exists because a browser's EventSource cannot set a header, so it cannot
// carry the session itself — and must not: the cookie is HttpOnly precisely so
// that page scripts never see it. The browser connects to this application's
// own origin, and the session is attached here, on the server.
//
// lastEventID is the browser's own doing. EventSource sends it when it
// reconnects, and it has to reach the API or the reconnection would start the
// run's output again from the beginning.
export async function apiStream(
  path: string,
  options: { signal?: AbortSignal; lastEventID?: string | null },
): Promise<Response> {
  const headers: Record<string, string> = { Accept: "text/event-stream" };

  const session = (await cookies()).get(sessionCookie);
  if (session) {
    headers.Cookie = `${sessionCookie}=${session.value}`;
  }
  if (options.lastEventID) {
    headers["Last-Event-ID"] = options.lastEventID;
  }

  return fetch(`${baseURL}${path}`, {
    cache: "no-store",
    headers,
    signal: options.signal,
  });
}
