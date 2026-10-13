import type { RunContext } from "../detail";
import { resultsOf } from "../metrics";
import type { Run } from "../types";
import { EndpointMetrics } from "./endpoint-metrics";
import { ExecutedScript } from "./executed-script";
import { HttpPhases } from "./http-phases";
import { RunHeader } from "./run-header";
import { RunLogs } from "./run-logs";
import { RunMetricsCards } from "./run-metrics-cards";
import { ThresholdList } from "./threshold-list";

export function RunDetail({ run, context }: { run: Run; context: RunContext }) {
  const results = resultsOf(run);

  return (
    <div className="flex flex-col gap-6">
      <RunHeader run={run} context={context} />
      <RunMetricsCards metrics={run.metrics} />
      <ThresholdList thresholds={run.thresholds} results={results} />
      <HttpPhases metrics={run.metrics} results={results} />
      <EndpointMetrics metrics={run.metrics} results={results} />
      <RunLogs runId={run.id} status={run.status} />
      <ExecutedScript version={context.executed} />
    </div>
  );
}
