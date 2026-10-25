import assert from "node:assert/strict";
import { test } from "node:test";

import { relayToApi } from "./relay.ts";

type Call = { url: string; init: RequestInit };

// A fake API: records what it was asked, answers what it is given.
function api(answer: Response) {
  const calls: Call[] = [];
  const fetchImpl = (async (url: URL | string, init: RequestInit) => {
    calls.push({ url: String(url), init });
    return answer;
  }) as typeof fetch;
  return { calls, fetchImpl };
}

const redirect = () => {
  const headers = new Headers({ Location: "https://github.com/login/oauth/authorize?x=1" });
  headers.append("Set-Cookie", "ballast_oauth=abc; Path=/auth/; HttpOnly; Secure; SameSite=Lax");
  headers.append("Set-Cookie", "ballast_session=jwt; Path=/; HttpOnly; Secure; SameSite=Lax");
  return new Response(null, { status: 302, headers });
};

test("a redirect is passed on whole: its status, its place and every cookie it sets", async () => {
  const { fetchImpl } = api(redirect());

  const res = await relayToApi(
    new Request("https://front.example.test/auth/github/login?next=%2Fx"),
    "http://api.internal:8080",
    "/auth/github/login",
    fetchImpl,
  );

  assert.equal(res.status, 302);
  assert.equal(res.headers.get("location"), "https://github.com/login/oauth/authorize?x=1");
  // Both, not the last: a sign-in sets two on its way through.
  assert.equal(res.headers.getSetCookie().length, 2);
  assert.equal(res.headers.get("cache-control"), "no-store");
});

test("the API is asked for the same path and the same query, and told not to follow", async () => {
  const { calls, fetchImpl } = api(redirect());

  await relayToApi(
    new Request("https://front.example.test/auth/github/callback?code=1&state=2"),
    "http://api.internal:8080",
    "/auth/github/callback",
    fetchImpl,
  );

  assert.equal(calls[0].url, "http://api.internal:8080/auth/github/callback?code=1&state=2");
  // Following it here would hand the browser the answer of the page it was sent to.
  assert.equal(calls[0].init.redirect, "manual");
  assert.equal(calls[0].init.method, "GET");
});

test("only the cookie goes to the API: nothing else the browser sent", async () => {
  const { calls, fetchImpl } = api(redirect());

  await relayToApi(
    new Request("https://front.example.test/auth/github/callback?code=1", {
      headers: {
        Cookie: "ballast_oauth=abc",
        Authorization: "Bearer should-not-travel",
        "X-Forwarded-For": "1.2.3.4",
        Host: "evil.example",
        Origin: "https://evil.example",
      },
    }),
    "http://api.internal:8080",
    "/auth/github/callback",
    fetchImpl,
  );

  const sent = new Headers(calls[0].init.headers);
  assert.equal(sent.get("cookie"), "ballast_oauth=abc");
  assert.deepEqual([...sent.keys()], ["cookie"]);
});

test("an answer that is not a redirect keeps its status and its type", async () => {
  const { fetchImpl } = api(
    new Response('{"error":"unknown provider"}', {
      status: 404,
      headers: { "Content-Type": "application/json", "Set-Cookie": "x=1" },
    }),
  );

  const res = await relayToApi(
    new Request("https://front.example.test/auth/nope/login"),
    "http://api.internal:8080",
    "/auth/nope/login",
    fetchImpl,
  );

  assert.equal(res.status, 404);
  assert.equal(res.headers.get("content-type"), "application/json");
  assert.equal(await res.text(), '{"error":"unknown provider"}');
});

test("an API that cannot be reached is a 502 that says nothing about where it was", async () => {
  const fetchImpl = (async () => {
    throw new Error("connect ECONNREFUSED api.internal:8080");
  }) as typeof fetch;

  const res = await relayToApi(
    new Request("https://front.example.test/auth/github/login"),
    "http://api.internal:8080",
    "/auth/github/login",
    fetchImpl,
  );

  assert.equal(res.status, 502);
  assert.ok(!(await res.text()).includes("api.internal"));
});
