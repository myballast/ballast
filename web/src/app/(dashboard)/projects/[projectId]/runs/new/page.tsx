import { getEnvironments } from "@/features/environments/api";
import { getAccess, getProject } from "@/features/projects/api";
import { LaunchPage } from "@/features/runs/components/launch-page";
import { getScenarios } from "@/features/scenarios/api";
import { can } from "@/lib/permissions";

export default async function NewRunPage(props: PageProps<"/projects/[projectId]/runs/new">) {
  const { projectId } = await props.params;
  const [project, access] = await Promise.all([getProject(projectId), getAccess(projectId)]);

  // Without run_test the page explains rather than offering a form the API
  // would refuse; it reads nothing else.
  const allowed = can(access, "run_test") && can(access, "view_project");
  const [scenarios, environments] = allowed
    ? await Promise.all([getScenarios(projectId), getEnvironments(projectId)])
    : [[], []];

  return <LaunchPage project={project} access={access} scenarios={scenarios} environments={environments} />;
}
