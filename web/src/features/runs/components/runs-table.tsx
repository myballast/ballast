import Link from "next/link";

import { Time } from "@/components/time";

import type { RunRow } from "../list";
import { RunStatusBadge } from "./run-status-badge";

const head = "px-4 py-2.5 font-normal";

// RunsTable is a page of a project's runs. The whole row opens the run: the
// link sits in the first cell and stretches over the row, so that the row is
// one target for a mouse and one stop for the keyboard.
//
// Every value that is a number or an identifier is in the monospace face with
// tabular figures, so the columns line up digit for digit. The status is a
// badge — an icon and a word, never a colour alone — and nothing else in the
// row is coloured: a high error rate is said by its figure, not by red.
export function RunsTable({ rows }: { rows: RunRow[] }) {
  return (
    <div className="overflow-x-auto rounded-xl border">
      <table className="w-full min-w-[56rem] text-sm">
        <thead>
          <tr className="border-b text-left text-[11px] tracking-wider text-muted-foreground uppercase">
            <th className={head}>Run</th>
            <th className={head}>Scenario</th>
            <th className={head}>Trigger</th>
            <th className={`${head} text-right`}>P95</th>
            <th className={`${head} text-right`}>Errors</th>
            <th className={`${head} text-right`}>VUs</th>
            <th className={`${head} text-right`}>Duration</th>
          </tr>
        </thead>
        <tbody>
          {rows.map((row) => (
            <tr key={row.id} className="relative border-b last:border-b-0 hover:bg-muted/40">
              <td className="px-4 py-2.5">
                <div className="flex items-center gap-3">
                  <RunStatusBadge status={row.status} />
                  <Link
                    href={`/runs/${row.id}`}
                    className="num font-medium after:absolute after:inset-0 focus-visible:outline-none focus-visible:after:ring-2 focus-visible:after:ring-ring"
                  >
                    #{row.number}
                    <span className="sr-only">, open this run</span>
                  </Link>
                </div>
                <p className="num mt-1 text-[11px] text-muted-foreground">
                  <Time iso={row.createdAt} />
                </p>
              </td>
              <td className="px-4 py-2.5">
                <p>{row.scenario}</p>
                <p className="data mt-1 text-[11px] text-muted-foreground">{row.environment}</p>
              </td>
              <td className="px-4 py-2.5 text-xs text-muted-foreground">{row.triggeredBy}</td>
              <td className="num px-4 py-2.5 text-right text-xs">{row.p95}</td>
              <td className="num px-4 py-2.5 text-right text-xs">{row.errors}</td>
              <td className="num px-4 py-2.5 text-right text-xs text-muted-foreground">{row.vus}</td>
              <td className="num px-4 py-2.5 text-right text-xs text-muted-foreground">{row.duration}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
