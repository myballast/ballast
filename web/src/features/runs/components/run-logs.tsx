"use client";

import { CircleAlert, Search } from "lucide-react";
import { useRouter } from "next/navigation";
import { useEffect, useMemo, useRef, useState } from "react";

import { cn } from "@/lib/utils";

import { levelOf, withoutLevel, type LogLevel } from "../log-level";
import type { LogLine as Line, RunStatus } from "../types";
import { LogLine } from "./log-line";
import { useRunTab } from "./run-tabs";

// How many lines are kept on screen. A soak test writes hundreds of thousands,
// and a browser holding every one of them in the DOM stops being a browser.
// The whole output remains in the database, and reloading replays it.
const keptOnScreen = 5000;

const time = new Intl.DateTimeFormat("en-GB", {
  hour: "2-digit",
  minute: "2-digit",
  second: "2-digit",
});

type Shown = { line: Line; level: LogLevel | null; clock: string };

// RunLogs follows a run's output as it is written, and lets it be searched.
//
// The connection is an EventSource against this application's own origin, which
// proxies the API: the session cookie is HttpOnly and no script here can read
// it, let alone send it. Reconnection is the browser's own doing — every event
// carries its line number as its id, so a dropped connection resumes exactly
// where it stopped, with no line twice.
//
// It stays mounted while another tab of the page is showing: the connection is
// what tells the page the run has started or ended, and it must not be dropped
// because somebody is reading the summary. Whether it is on screen matters to
// one thing — following the newest line needs a pane that has a height.
export function RunLogs({
  runId,
  status,
}: {
  runId: string;
  status: RunStatus;
}) {
  const active = useRunTab() === "output";
  const [lines, setLines] = useState<Shown[]>([]);
  const [live, setLive] = useState(true);
  const [failed, setFailed] = useState(false);
  const [query, setQuery] = useState("");
  const [errorsOnly, setErrorsOnly] = useState(false);

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
    const source = new EventSource(
      `/api/runs/${encodeURIComponent(runId)}/logs`,
    );

    source.addEventListener("line", (event) => {
      const line = JSON.parse((event as MessageEvent<string>).data) as Line;
      if (line.seq <= highest.current) return;
      highest.current = line.seq;

      setFailed(false);
      setLines((previous) =>
        [
          ...previous,
          {
            line,
            level: levelOf(line.message),
            clock: time.format(new Date(line.at)),
          },
        ].slice(-keptOnScreen),
      );

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

  const needle = query.trim().toLowerCase();
  const visible = useMemo(
    () =>
      lines.filter(
        (l) =>
          (!errorsOnly || l.level === "error") &&
          (needle === "" || l.line.message.toLowerCase().includes(needle)),
      ),
    [lines, errorsOnly, needle],
  );
  const filtering = needle !== "" || errorsOnly;
  const errors = useMemo(
    () => lines.filter((l) => l.level === "error").length,
    [lines],
  );

  return (
    <div className="flex flex-col overflow-hidden rounded-xl border bg-background">
      <div className="flex flex-wrap items-center gap-x-3 gap-y-2 border-b bg-card px-3 py-2">
        <label className="relative">
          <span className="sr-only">Search the output</span>
          <Search
            aria-hidden
            className="pointer-events-none absolute top-1/2 left-2.5 size-3.5 -translate-y-1/2 text-muted-foreground"
          />
          <input
            type="search"
            value={query}
            onChange={(event) => setQuery(event.target.value)}
            placeholder="Search the output"
            className="data h-8 w-52 rounded-md border bg-background pr-2 pl-8 text-xs outline-none focus-visible:ring-2 focus-visible:ring-ring"
          />
        </label>

        <span role="status" className="data text-[11px] text-muted-foreground">
          {filtering ? (
            <>
              <span className="num">{visible.length}</span>{" "}
              {visible.length === 1 ? "line matches" : "lines match"}
            </>
          ) : failed ? (
            "reconnecting…"
          ) : live ? (
            <>
              following · <span className="num">{lines.length}</span> lines
            </>
          ) : (
            <>
              <span className="num">{lines.length}</span> lines
            </>
          )}
        </span>

        {/* The red of a failed run is not borrowed for this button: it says what
            it does by its label, and it is pressed or not. */}
        <button
          type="button"
          aria-pressed={errorsOnly}
          onClick={() => setErrorsOnly((on) => !on)}
          className={cn(
            "ml-auto flex h-8 items-center gap-1.5 rounded-md border px-2.5 text-xs text-muted-foreground hover:text-foreground",
            errorsOnly && "border-foreground/40 bg-muted text-foreground",
          )}
        >
          <CircleAlert aria-hidden className="size-3.5" />
          Errors only
          <span className="num text-[11px]">{errors}</span>
        </button>
      </div>

      {lines.length === 0 ? (
        <p className="px-4 py-6 text-sm text-muted-foreground">
          {status === "queued"
            ? "This run has not started yet."
            : "No output yet."}
        </p>
      ) : visible.length === 0 ? (
        <p className="px-4 py-6 text-sm text-muted-foreground">
          No line matches{needle ? ` “${query.trim()}”` : ""}
          {errorsOnly ? " among the errors" : ""}.
        </p>
      ) : (
        <LogPane lines={visible} needle={needle} active={active} />
      )}
    </div>
  );
}

// LogPane is the scrolling area. It follows the newest line while the reader is
// at the bottom, and stops the moment they scroll up — reading what just went
// past is the reason somebody scrolls up, and yanking them back down is how a
// log view becomes unusable.
function LogPane({
  lines,
  needle,
  active,
}: {
  lines: Shown[];
  needle: string;
  active: boolean;
}) {
  const pane = useRef<HTMLDivElement>(null);
  const following = useRef(true);

  useEffect(() => {
    const element = pane.current;
    if (!element || !active || !following.current) return;

    element.scrollTop = element.scrollHeight;
  }, [lines, active]);

  return (
    <div
      ref={pane}
      onScroll={(event) => {
        const element = event.currentTarget;
        const fromBottom =
          element.scrollHeight - element.scrollTop - element.clientHeight;
        following.current = fromBottom < 24;
      }}
      className="data max-h-[32rem] overflow-auto py-2 text-xs leading-5"
      role="log"
      aria-live="off"
      aria-label="Run output"
    >
      <div className="min-w-[40rem]">
        {lines.map(({ line, level, clock }) => (
          <LogLine key={line.seq} seq={line.seq} time={clock} level={level}>
            <Marked
              text={level ? withoutLevel(line.message) : line.message}
              needle={needle}
            />
          </LogLine>
        ))}
      </div>
    </div>
  );
}

// Marked sets what a search matched in a mark, so that it can be found in a long
// line. A mark is reversed text, not a colour, and has no meaning of its own.
function Marked({ text, needle }: { text: string; needle: string }) {
  if (needle === "") return text;

  const parts: React.ReactNode[] = [];
  const lower = text.toLowerCase();
  let from = 0;
  for (
    let at = lower.indexOf(needle);
    at !== -1;
    at = lower.indexOf(needle, from)
  ) {
    if (at > from) parts.push(text.slice(from, at));
    parts.push(
      <mark key={at} className="bg-foreground text-background">
        {text.slice(at, at + needle.length)}
      </mark>,
    );
    from = at + needle.length;
  }
  parts.push(text.slice(from));
  return parts;
}
