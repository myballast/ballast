"use client";

import { useRouter } from "next/navigation";
import { useEffect, useRef, useState } from "react";

import {
  Card,
  CardAction,
  CardContent,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";

import type { LogLine, RunStatus } from "../types";

// How many lines are kept on screen. A soak test writes hundreds of thousands,
// and a browser holding every one of them in the DOM stops being a browser.
// The whole output remains in the database, and reloading replays it.
const keptOnScreen = 5000;

// RunLogs follows a run's output as it is written.
//
// The connection is an EventSource against this application's own origin, which
// proxies the API: the session cookie is HttpOnly and no script here can read
// it, let alone send it. Reconnection is the browser's own doing — every event
// carries its line number as its id, so a dropped connection resumes exactly
// where it stopped, with no line twice.
export function RunLogs({ runId, status }: { runId: string; status: RunStatus }) {
  const [lines, setLines] = useState<LogLine[]>([]);
  const [live, setLive] = useState(true);
  const [failed, setFailed] = useState(false);

  // The highest line number already on screen. A defence of its own: the
  // resumption is exact, and this makes a duplicate impossible rather than
  // unlikely.
  const highest = useRef(-1);

  // The rest of the page is rendered on the server, from the run as it was
  // when the page was asked for. When the output says the run has moved on —
  // its first line, or its end — the page is rendered again, so that its
  // status, dates and metrics are the current ones without a reload.
  const router = useRouter();
  const shown = useRef(status);

  useEffect(() => {
    const source = new EventSource(`/api/runs/${encodeURIComponent(runId)}/logs`);

    source.addEventListener("line", (event) => {
      const line = JSON.parse((event as MessageEvent<string>).data) as LogLine;
      if (line.seq <= highest.current) return;
      highest.current = line.seq;

      setFailed(false);
      setLines((previous) => [...previous, line].slice(-keptOnScreen));

      if (shown.current === "queued") {
        shown.current = "running";
        router.refresh();
      }
    });

    // The run is over: there will never be another line, so the stream is
    // closed here rather than left to reconnect for ever.
    source.addEventListener("end", () => {
      setLive(false);
      source.close();

      if (shown.current === "queued" || shown.current === "running") {
        router.refresh();
      }
    });

    // An error is not the end: EventSource reconnects on its own, and says so
    // again when it has. Shown as a notice rather than as a failure.
    source.addEventListener("error", () => setFailed(true));

    return () => source.close();
  }, [runId, router]);

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-[11px] font-normal tracking-wider text-muted-foreground uppercase">
          Output
        </CardTitle>
        {/* CardAction is what puts this on the right: the header is a grid that
            grows a second column when one is present. */}
        <CardAction className="data text-[11px] text-muted-foreground">
          {failed
            ? "reconnecting…"
            : live
              ? `following · ${lines.length} lines`
              : `${lines.length} lines`}
        </CardAction>
      </CardHeader>
      <CardContent>
        {lines.length === 0 ? (
          <p className="text-sm text-muted-foreground">
            {status === "queued"
              ? "This run has not started yet."
              : "No output yet."}
          </p>
        ) : (
          <LogPane lines={lines} />
        )}
      </CardContent>
    </Card>
  );
}

// LogPane is the scrolling area. It follows the newest line while the reader is
// at the bottom, and stops the moment they scroll up — reading what just went
// past is the reason somebody scrolls up, and yanking them back down is how a
// log view becomes unusable.
function LogPane({ lines }: { lines: LogLine[] }) {
  const pane = useRef<HTMLDivElement>(null);
  const following = useRef(true);

  useEffect(() => {
    const element = pane.current;
    if (!element || !following.current) return;

    element.scrollTop = element.scrollHeight;
  }, [lines]);

  return (
    <div
      ref={pane}
      onScroll={(event) => {
        const element = event.currentTarget;
        const fromBottom =
          element.scrollHeight - element.scrollTop - element.clientHeight;
        following.current = fromBottom < 24;
      }}
      className="data max-h-96 overflow-y-auto rounded-md border bg-background p-3 text-xs leading-5"
      role="log"
      aria-live="polite"
      aria-label="Run output"
    >
      {lines.map((line) => (
        <div key={line.seq} className="flex gap-3 whitespace-pre-wrap">
          <span className="w-10 shrink-0 text-right text-muted-foreground select-none">
            {line.seq}
          </span>
          {/* stderr is marked with a word, never a colour: red, amber and green
              belong to a run's status and to a threshold's verdict. */}
          {line.stream === "stderr" && (
            <span className="shrink-0 text-muted-foreground uppercase">err</span>
          )}
          <span className="min-w-0">{line.message}</span>
        </div>
      ))}
    </div>
  );
}
