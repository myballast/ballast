import { NotAllowed } from "@/components/not-allowed";
import { getCatalogOrNothing } from "@/features/endpoints/api";
import { getAccess, getProject } from "@/features/projects/api";
import { NewScenarioPage } from "@/features/scenarios/components/new-scenario-page";
import { can } from "@/lib/permissions";

export default async function NewScenarioRoute(props: PageProps<"/projects/[projectId]/scenarios/new">) {
  const { projectId } = await props.params;
  const [project, access] = await Promise.all([getProject(projectId), getAccess(projectId)]);

  // Explained rather than offered: the API would refuse the form.
  if (!can(access, "manage_scenarios")) {
    return <NotAllowed>Writing a scenario takes the maintainer role on this project.</NotAllowed>;
  }

  return <NewScenarioPage project={project} endpoints={await getCatalogOrNothing(projectId)} />;
}
