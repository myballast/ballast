import { ScenarioDetail } from "@/features/scenarios/components/scenario-detail";
import { loadScenarioPage } from "@/features/scenarios/detail";

export default async function ScenarioRoute(props: PageProps<"/projects/[projectId]/scenarios/[scenarioId]">) {
  const { projectId, scenarioId } = await props.params;
  return <ScenarioDetail page={await loadScenarioPage(projectId, scenarioId)} />;
}
