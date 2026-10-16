import type { RunContext } from "../detail";
import { resultsOf } from "../metrics";
import type { Run } from "../types";
import { EndpointMetrics } from "./endpoint-metrics";
import { ExecutedScript } from "./executed-script";
import { HttpPhases } from "./http-phases";
import { RunExecution } from "./run-execution";
import { RunHeader } from "./run-header";
import { RunLogs } from "./run-logs";
import { RunMetricsCards } from "./run-metrics-cards";
import { RunTabs, type RunTab } from "./run-tabs";
import { ThresholdList } from "./threshold-list";

// RunDetail is a run: who and what in the header, then two tabs — what it
// measured, and what it said while it ran. Metrics, steps, artifacts and an
// analysis are not tabs here: none of them exists yet.
export function RunDetail({ run, context, tab }: { run: Run; context: RunContext; tab: RunTab }) {
  const results = resultsOf(run);

  return (
    <div className="flex flex-col gap-6">
      <RunHeader run={run} context={context} now={new Date()} />
      <RunTabs
        initial={tab}
        summary={
          <>
            <RunExecution run={run} context={context} />
            <RunMetricsCards metrics={run.metrics} />
            <ThresholdList thresholds={run.thresholds} results={results} />
            <HttpPhases metrics={run.metrics} results={results} />
            <EndpointMetrics metrics={run.metrics} results={results} />
            <ExecutedScript version={context.executed} />
          </>
        }
        output={<RunLogs runId={run.id} status={run.status} />}
      />
    </div>
  );
}
