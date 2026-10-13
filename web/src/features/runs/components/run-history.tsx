"use client";

import Link from "next/link";
import { useState, useTransition } from "react";

import { EmptyState } from "@/components/states";
import { Button } from "@/components/ui/button";

import { loadOlderRuns } from "../actions";
import type { HistoryPage, HistoryRow, Names } from "../history";
import { RunStatusBadge } from "./run-status-badge";

// RunHistory is a project's runs, newest first, a page at a time. A client
// component for one reason: "Load older runs" appends to what is shown. The
// rows arrive already formatted by the server.
export function RunHistory({
  projectId,
  names,
  first,
}: {
  projectId: string;
  names: Names;
  first: HistoryPage;
}) {
  const [rows, setRows] = useState<HistoryRow[]>(first.rows);
  const [next, setNext] = useState(first.next);
  const [failed, setFailed] = useState(false);
  const [pending, startTransition] = useTransition();

  if (rows.length === 0) {
    return <EmptyState title="No run yet" description="Runs appear here once a scenario has been launched." />;
  }

  const loadMore = () =>
    startTransition(async () => {
      if (!next) return;
      try {
        const page = await loadOlderRuns(projectId, names, next);
        setRows((shown) => [...shown, ...page.rows]);
        setNext(page.next);
        setFailed(false);
      } catch {
        setFailed(true);
      }
    });

  return (
    <div className="flex flex-col gap-3">
      <div className="overflow-x-auto rounded-xl border">
        <table className="w-full min-w-[40rem] text-sm">
          <thead>
            <tr className="border-b text-left text-[11px] tracking-wider text-muted-foreground uppercase">
              <th className="px-4 py-2.5 font-normal">Status</th>
              <th className="px-4 py-2.5 font-normal">Scenario</th>
              <th className="px-4 py-2.5 font-normal">Environment</th>
              <th className="px-4 py-2.5 font-normal">Profile</th>
              <th className="px-4 py-2.5 font-normal">Started</th>
              <th className="px-4 py-2.5 font-normal">
                <span className="sr-only">Open</span>
              </th>
            </tr>
          </thead>
          <tbody>
            {rows.map((row) => (
              <tr key={row.id} className="border-b last:border-b-0">
                <td className="px-4 py-2">
                  <RunStatusBadge status={row.status} />
                </td>
                <td className="px-4 py-2">{row.scenario}</td>
                <td className="data px-4 py-2 text-xs">{row.environment}</td>
                <td className="px-4 py-2 text-muted-foreground">{row.profile}</td>
                <td className="num px-4 py-2 text-xs text-muted-foreground">{row.createdAt}</td>
                <td className="px-4 py-2 text-right">
                  <Link href={`/runs/${row.id}`} className="text-xs text-brand hover:underline">
                    View
                  </Link>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      {next && (
        <Button variant="outline" onClick={loadMore} disabled={pending} className="self-center">
          {pending ? "Loading…" : "Load older runs"}
        </Button>
      )}
      {failed && (
        <p role="alert" className="self-center text-sm text-muted-foreground">
          Older runs could not be loaded. Try again.
        </p>
      )}
    </div>
  );
}
