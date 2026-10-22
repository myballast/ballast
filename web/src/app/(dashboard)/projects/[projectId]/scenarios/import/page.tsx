import { NotAllowed } from "@/components/not-allowed";
import { getEnvironments } from "@/features/environments/api";
import { getAccess, getProject } from "@/features/projects/api";
import { ImportPage, type ImportSource } from "@/features/scenarios/components/import-page";
import { can } from "@/lib/permissions";

const sources: ImportSource[] = ["openapi", "postman", "har", "curl"];

function sourceOf(from: string | string[] | undefined): ImportSource {
  return sources.find((source) => source === from) ?? "openapi";
}

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
      from={sourceOf(from)}
      environments={environments.map((environment) => ({ id: environment.id, name: environment.name }))}
      canManageEnvironments={canManageEnvironments}
    />
  );
}
