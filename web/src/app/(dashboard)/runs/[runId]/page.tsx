import { notFound } from "next/navigation";

import { getRun } from "@/features/runs/api";
import { RunDetail } from "@/features/runs/components/run-detail";

export default async function RunPage(props: PageProps<"/runs/[runId]">) {
  const { runId } = await props.params;

  const run = await getRun(runId);
  if (!run) notFound();

  return <RunDetail run={run} />;
}
