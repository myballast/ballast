import { cookies } from "next/headers";

// The only way the front talks to the Go API.
//
// It runs on the server: the base URL is not prefixed with NEXT_PUBLIC_, so it
// never reaches the browser. The browser's session cookie is forwarded to the
// API on its behalf; page scripts never see it, since it is HttpOnly.
const baseURL = process.env.BALLAST_API_URL ?? "http://localhost:8080";

// Where this server reaches the API, for the route handlers that relay to it. The
// browser never gets an address of the API: it signs in through this front end,
// so that the session cookie is set on the host that reads it.
export const apiBaseURL = baseURL;

// Must match session.CookieName in internal/adapter/http/session.
export const sessionCookie = "ballast_session";

export class ApiError extends Error {
  constructor(
    readonly status: number,
    message: string,
    // The whole failed response, for the few that carry more than a sentence:
    // a refused script says what was wrong with it, line by line.
    readonly body?: unknown,
  ) {
    super(message);
    this.name = "ApiError";
  }
}

export async function apiGet<T>(path: string): Promise<T> {
  return request<T>("GET", path);
}

// apiPost sends body as JSON. It is called from server actions only, which is
// where the session cookie can be read and forwarded.
export async function apiPost<T>(path: string, body: unknown): Promise<T> {
  return request<T>("POST", path, body);
}

// apiPut and apiPatch send body as JSON. Like apiPost, from server actions only.
export async function apiPut<T>(path: string, body: unknown): Promise<T> {
  return request<T>("PUT", path, body);
}

export async function apiPatch<T>(path: string, body: unknown): Promise<T> {
  return request<T>("PATCH", path, body);
}

// apiDelete sends a DELETE. Like apiPost, from server actions only. Some
// deletions answer 204 with no body, which resolves to undefined.
export async function apiDelete<T = undefined>(path: string): Promise<T> {
  return request<T>("DELETE", path);
}

// apiPostAndAdoptSession is for the two calls whose answer is a new session —
// accepting an invitation and switching organization. The API sets the cookie
// on its response; the browser never sees that response, this server does, so
// the cookie is set again on the response the browser will get. Server actions
// only: nowhere else may a cookie be written.
export async function apiPostAndAdoptSession<T>(path: string, body: unknown): Promise<T> {
  return request<T>("POST", path, body, true);
}

// adoptSession copies the session cookie of an API response onto this
// application's own response, with the same lifetime. The flags are the ones
// the API sets: HttpOnly, so no script reads it, and Lax.
async function adoptSession(response: Response): Promise<void> {
  for (const header of response.headers.getSetCookie()) {
    const [pair, ...attributes] = header.split(";").map((part) => part.trim());
    const [name, ...rest] = pair.split("=");
    if (name !== sessionCookie) continue;

    const maxAge = attributes.find((a) => a.toLowerCase().startsWith("max-age="));
    (await cookies()).set({
      name,
      value: rest.join("="),
      httpOnly: true,
      secure: true,
      sameSite: "lax",
      path: "/",
      maxAge: maxAge ? Number.parseInt(maxAge.split("=")[1], 10) : undefined,
    });
  }
}

async function request<T>(method: string, path: string, body?: unknown, adopt = false): Promise<T> {
  const headers: Record<string, string> = { Accept: "application/json" };

  const session = (await cookies()).get(sessionCookie);
  if (session) {
    headers.Cookie = `${sessionCookie}=${session.value}`;
  }
  if (body !== undefined) {
    headers["Content-Type"] = "application/json";
  }

  const response = await fetch(`${baseURL}${path}`, {
    method,
    cache: "no-store",
    headers,
    body: body === undefined ? undefined : JSON.stringify(body),
  });

  if (!response.ok) {
    // Every failed response from the API carries {"error": "..."}.
    const failure = (await response.json().catch(() => null)) as {
      error?: string;
    } | null;
    throw new ApiError(response.status, failure?.error ?? response.statusText, failure ?? undefined);
  }

  if (adopt) {
    await adoptSession(response);
  }

  if (response.status === 204) {
    return undefined as T;
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
