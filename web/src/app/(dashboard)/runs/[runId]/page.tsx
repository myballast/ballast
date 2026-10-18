import { notFound } from "next/navigation";

import { getRun } from "@/features/runs/api";
import { RunDetail } from "@/features/runs/components/run-detail";
import { loadRunContext } from "@/features/runs/detail";

export default async function RunPage(props: PageProps<"/runs/[runId]">) {
  const { runId } = await props.params;
  const { tab } = await props.searchParams;

  const run = await getRun(runId);
  if (!run) notFound();

  // The tab is in the address, so a link can open on the output.
  return <RunDetail run={run} context={await loadRunContext(run)} tab={tab === "output" ? "output" : "summary"} />;
}
