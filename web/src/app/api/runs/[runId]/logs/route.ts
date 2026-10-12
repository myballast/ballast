import type { NextRequest } from "next/server";

import { apiStream } from "@/lib/api-client";

// The live log stream, proxied.
//
// The browser opens an EventSource against this application, not against the
// API, for one reason: EventSource cannot set a header, so it cannot send the
// session — and it must not be able to. The cookie is HttpOnly, no page script
// ever reads it, and the token stays on the server where it is attached to the
// outgoing request.
//
// Nothing is cached and nothing is buffered. A Route Handler is not cached by
// default, and this one reads cookies, so it runs per request by nature — no
// segment configuration is needed to say so.
export async function GET(
  request: NextRequest,
  context: RouteContext<"/api/runs/[runId]/logs">,
) {
  const { runId } = await context.params;

  const upstream = await apiStream(
    `/runs/${encodeURIComponent(runId)}/logs`,
    {
      // The browser closing the stream has to close the one behind it, or a
      // reader would be left following a run nobody is watching.
      signal: request.signal,
      lastEventID: request.headers.get("Last-Event-ID"),
    },
  );

  // A refusal is passed through as it came: a status other than 200 is what
  // tells EventSource to give up rather than reconnect for ever, and the body
  // is the API's own {"error": "..."}.
  if (!upstream.ok || !upstream.body) {
    return new Response(upstream.body, {
      status: upstream.status,
      headers: {
        "Content-Type": upstream.headers.get("Content-Type") ?? "application/json",
      },
    });
  }

  return new Response(upstream.body, {
    status: 200,
    headers: {
      "Content-Type": "text/event-stream; charset=utf-8",
      // no-transform as well as no-cache: a proxy that "optimises" a response
      // by buffering it turns a live stream into one long silence.
      "Cache-Control": "no-cache, no-transform",
      Connection: "keep-alive",
      "X-Accel-Buffering": "no",
    },
  });
}
