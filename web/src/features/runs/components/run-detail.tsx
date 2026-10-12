import type { Run } from "../types";
import { RunHeader } from "./run-header";
import { RunLogs } from "./run-logs";
import { RunMetricsCards } from "./run-metrics-cards";
import { ThresholdList } from "./threshold-list";

export function RunDetail({ run }: { run: Run }) {
  return (
    <div className="flex flex-col gap-6">
      <RunHeader run={run} />
      <RunMetricsCards metrics={run.metrics} />
      <RunLogs runId={run.id} status={run.status} />
      <ThresholdList thresholds={run.thresholds} />
    </div>
  );
}
