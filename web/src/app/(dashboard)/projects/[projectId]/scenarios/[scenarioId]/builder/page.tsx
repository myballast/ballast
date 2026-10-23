import { redirect } from "next/navigation";

import { BuilderPage } from "@/features/builder/components/builder-page";
import { loadScenarioPage } from "@/features/scenarios/detail";

export default async function BuilderRoute(props: PageProps<"/projects/[projectId]/scenarios/[scenarioId]/builder">) {
  const { projectId, scenarioId } = await props.params;
  const page = await loadScenarioPage(projectId, scenarioId);

  // Only a scenario written as a graph has anything to draw; one written as code
  // is edited on its own page.
  if (page.scenario.mode !== "graph" || !page.scenario.graph) redirect(`/projects/${projectId}/scenarios/${scenarioId}`);

  return <BuilderPage page={page} />;
}
