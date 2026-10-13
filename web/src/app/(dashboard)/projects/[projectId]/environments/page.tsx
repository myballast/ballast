import { getEnvironments } from "@/features/environments/api";
import { EnvironmentsPage } from "@/features/environments/components/environments-page";
import { ProjectHeader, RestrictedProject } from "@/features/projects/components/project-overview";
import { getAccess, getProject } from "@/features/projects/api";
import { can } from "@/lib/permissions";

export default async function EnvironmentsRoute(props: PageProps<"/projects/[projectId]/environments">) {
  const { projectId } = await props.params;
  const [project, access] = await Promise.all([getProject(projectId), getAccess(projectId)]);

  if (!can(access, "view_project")) {
    return (
      <div className="flex flex-col gap-6">
        <ProjectHeader project={project} access={access} />
        <RestrictedProject />
      </div>
    );
  }

  return <EnvironmentsPage project={project} access={access} environments={await getEnvironments(projectId)} />;
}
