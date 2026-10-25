// Hands a sign-in step to the API and gives the browser its answer as it came.
//
// A sign-in is a conversation between the browser and the API: it starts with a
// redirect, comes back from the provider with a code, and ends with a session
// cookie. If the browser spoke to the API directly, that cookie would belong to
// the API's host, and this front end — on another host of the same platform,
// which cannot share it — would never see it. Relaying keeps the browser on one
// origin, so the cookie is set on the host that reads it.
//
// Written against the standard Request and Response, with the fetch it uses
// passed in, so that it runs under a plain test.

// What a sign-in answers with, other than what is passed on whole.
const passedOn = ["location", "content-type"];

export async function relayToApi(
  request: Request,
  apiBase: string,
  path: string,
  fetchImpl: typeof fetch = fetch,
): Promise<Response> {
  const target = new URL(path, apiBase);
  target.search = new URL(request.url).search;

  // Only the cookie, which carries the sign-in's state and verifier. Whatever
  // else the browser sent — a header that names another host, an Authorization,
  // forwarding hints — is not the API's business, and is not forwarded.
  const headers = new Headers();
  const cookie = request.headers.get("cookie");
  if (cookie) headers.set("cookie", cookie);

  let upstream: Response;
  try {
    // Never followed: the redirect is the answer, and it is for the browser.
    upstream = await fetchImpl(target, { method: "GET", headers, redirect: "manual" });
  } catch {
    // The reason stays in this server's log; the browser is not told where the
    // API lives.
    return new Response("The sign-in service is not reachable.", { status: 502 });
  }

  const answer = new Headers({ "Cache-Control": "no-store" });
  for (const name of passedOn) {
    const value = upstream.headers.get(name);
    if (value) answer.set(name, value);
  }
  // Every cookie, not one: `get` would fold them into a single line.
  for (const cookieLine of upstream.headers.getSetCookie()) {
    answer.append("Set-Cookie", cookieLine);
  }

  const isRedirect = upstream.status >= 300 && upstream.status < 400;
  return new Response(isRedirect ? null : upstream.body, { status: upstream.status, headers: answer });
}
