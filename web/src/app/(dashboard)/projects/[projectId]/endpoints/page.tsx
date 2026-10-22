import { getCatalog } from "@/features/endpoints/api";
import { EndpointsPage } from "@/features/endpoints/components/endpoints-page";
import { ProjectHeader, RestrictedProject } from "@/features/projects/components/project-overview";
import { getAccess, getProject } from "@/features/projects/api";
import { can } from "@/lib/permissions";

export default async function EndpointsRoute(props: PageProps<"/projects/[projectId]/endpoints">) {
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

  return <EndpointsPage project={project} access={access} endpoints={await getCatalog(projectId)} />;
}
