import { NotAllowed } from "@/components/not-allowed";
import { getAccess, getProject } from "@/features/projects/api";
import { OpenAPIImportPage } from "@/features/scenarios/components/openapi-import-page";
import { can } from "@/lib/permissions";

export default async function ImportOpenAPIRoute(props: PageProps<"/projects/[projectId]/scenarios/import">) {
  const { projectId } = await props.params;
  const [project, access] = await Promise.all([getProject(projectId), getAccess(projectId)]);

  // Explained rather than offered: the API would refuse the import.
  if (!can(access, "manage_scenarios")) {
    return <NotAllowed>Writing a scenario takes the maintainer role on this project.</NotAllowed>;
  }

  return <OpenAPIImportPage project={project} />;
}
