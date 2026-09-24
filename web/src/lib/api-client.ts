// The only way the front talks to the Go API.
//
// It runs on the server: the base URL is not prefixed with NEXT_PUBLIC_, so it
// never reaches the browser, and neither will the token once authentication
// exists.
const baseURL = process.env.BALLAST_API_URL ?? "http://localhost:8080";

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
  const response = await fetch(`${baseURL}${path}`, {
    cache: "no-store",
    headers: { Accept: "application/json" },
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
