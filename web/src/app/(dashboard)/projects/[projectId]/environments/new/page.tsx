import { EnvironmentFormPage } from "@/features/environments/components/environment-form-page";
import { NotAllowed } from "@/features/environments/components/not-allowed";
import { getAccess, getProject } from "@/features/projects/api";
import { can } from "@/lib/permissions";

export default async function NewEnvironmentRoute(props: PageProps<"/projects/[projectId]/environments/new">) {
  const { projectId } = await props.params;
  const [project, access] = await Promise.all([getProject(projectId), getAccess(projectId)]);

  // Explained rather than offered: the API would refuse the form.
  if (!can(access, "manage_environments")) {
    return <NotAllowed>Declaring an environment takes the maintainer role on this project.</NotAllowed>;
  }

  return <EnvironmentFormPage project={project} />;
}
