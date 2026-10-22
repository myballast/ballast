import { NotAllowed } from "@/components/not-allowed";
import { getEnvironments } from "@/features/environments/api";
import { getAccess, getProject } from "@/features/projects/api";
import { ImportPage } from "@/features/scenarios/components/import-page";
import { can } from "@/lib/permissions";

export default async function ImportRoute(props: PageProps<"/projects/[projectId]/scenarios/import">) {
  const { projectId } = await props.params;
  const { from } = await props.searchParams;
  const [project, access] = await Promise.all([getProject(projectId), getAccess(projectId)]);

  // Explained rather than offered: the API would refuse the import.
  if (!can(access, "manage_scenarios")) {
    return <NotAllowed>Writing a scenario takes the maintainer role on this project.</NotAllowed>;
  }

  // Only somebody who may create secrets is offered the environments to put them in.
  const canManageEnvironments = can(access, "manage_environments");
  const environments = canManageEnvironments ? await getEnvironments(projectId) : [];

  return (
    <ImportPage
      project={project}
      from={from === "postman" ? "postman" : "openapi"}
      environments={environments.map((environment) => ({ id: environment.id, name: environment.name }))}
      canManageEnvironments={canManageEnvironments}
    />
  );
}
