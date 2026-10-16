"use client";

import { RotateCw } from "lucide-react";

import { ConfirmAction } from "@/components/confirm-action";

import { rerunRun } from "../actions";

// RerunRun repeats the run, after asking. What it asks says what will be
// repeated, since that is the point of it: the new run is not today's scenario
// run with today's defaults, it is this run again.
export function RerunRun({
  runId,
  scenario,
  version,
  profile,
  load,
}: {
  runId: string;
  scenario: string;
  version: number;
  profile: string;
  load: string;
}) {
  return (
    <ConfirmAction
      icon={<RotateCw aria-hidden />}
      label="Re-run"
      question={
        <>
          Run <span className="font-medium">{scenario}</span> <span className="data text-xs">v{version}</span> again,
          with the same <span className="data text-xs">{profile}</span> profile and the same load (
          <span className="num text-xs">{load}</span>)? It uses what this run froze, not what the scenario is now. The
          environment&apos;s ceilings and hours still apply.
        </>
      }
      confirm="Re-run"
      keep="Cancel"
      busy="Queuing…"
      act={() => rerunRun(runId)}
    />
  );
}
