import { getEnvironment } from "@/features/environments/api";
import { EnvironmentFormPage } from "@/features/environments/components/environment-form-page";
import { NotAllowed } from "@/features/environments/components/not-allowed";
import { getAccess, getProject } from "@/features/projects/api";
import { can } from "@/lib/permissions";

export default async function EditEnvironmentRoute(
  props: PageProps<"/projects/[projectId]/environments/[environmentId]/edit">,
) {
  const { projectId, environmentId } = await props.params;
  const [project, access, environment] = await Promise.all([
    getProject(projectId),
    getAccess(projectId),
    getEnvironment(projectId, environmentId),
  ]);

  if (!can(access, "manage_environments")) {
    return <NotAllowed>Editing an environment takes the maintainer role on this project.</NotAllowed>;
  }

  return <EnvironmentFormPage project={project} environment={environment} />;
}
