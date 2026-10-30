import { CircleCheck, CircleOff, Hourglass, Server, type LucideIcon } from "lucide-react";

import { EmptyState } from "@/components/states";
import { cn } from "@/lib/utils";

import { outdatedNote, since, statusLabel } from "../status";
import type { Runner, RunnerList, RunnerStatus } from "../types";

// A runner's state is not the status of a run or a threshold, so it takes none of
// their colours: an icon and a word, in neutral text, as a verification does.
const icons: Record<RunnerStatus, LucideIcon> = {
  online: CircleCheck,
  draining: Hourglass,
  offline: CircleOff,
};

function RunnerRow({ runner, minimum, now }: { runner: Runner; minimum: number; now: Date }) {
  const Icon = icons[runner.status];
  const note = outdatedNote(runner, minimum);

  return (
    <li className="flex flex-col gap-1 border-b px-4 py-3 last:border-b-0">
      <div className="flex flex-wrap items-center gap-x-6 gap-y-1">
        <span className="data min-w-0 flex-1 truncate text-[12.5px]" title={runner.name}>
          {runner.name}
        </span>
        <span
          className={cn(
            "flex items-center gap-1 text-xs",
            runner.status === "offline" ? "text-muted-foreground" : "text-foreground",
          )}
        >
          <Icon aria-hidden className="size-3.5 shrink-0" />
          {statusLabel(runner.status)}
        </span>
        <span className="data text-[11.5px] text-muted-foreground" title="Build">
          {runner.version}
        </span>
        <span className="num text-[11.5px] text-muted-foreground" title="Contract">
          contract {runner.contract}
        </span>
        <span className="text-[11.5px] text-muted-foreground">
          Last seen <time dateTime={runner.last_seen_at}>{since(runner.last_seen_at, now)}</time>
        </span>
      </div>
      {note && <p className="text-xs text-muted-foreground">{note}</p>}
    </li>
  );
}

// RunnerCard is the machines that execute this organization's runs, and whether
// each one is there. A runner says so itself, every ten seconds; "offline" is
// read from how long ago it last spoke.
//
// Read-only on purpose: nothing here can start, stop or register a runner.
export function RunnerCard({ list, now }: { list: RunnerList; now: Date }) {
  return (
    <section aria-labelledby="runners-heading" className="flex flex-col gap-3">
      <div className="flex flex-col gap-1">
        <h2 id="runners-heading" className="text-lg font-medium tracking-tight">
          Runners
        </h2>
        <p className="max-w-xl text-sm text-muted-foreground">
          The machines that execute the organization&apos;s runs. A run waits in the queue while none is online.
        </p>
      </div>

      {list.runners.length === 0 ? (
        <EmptyState
          icon={Server}
          title="No runner has reported yet"
          description="A runner appears here as soon as it starts. Until then, runs stay queued."
        />
      ) : (
        <ul className="rounded-xl border bg-card">
          {list.runners.map((runner) => (
            <RunnerRow key={runner.name} runner={runner} minimum={list.minimum_contract} now={now} />
          ))}
        </ul>
      )}
    </section>
  );
}
