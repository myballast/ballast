import { ProjectHeader, RestrictedProject } from "@/features/projects/components/project-overview";
import { getAccess, getProject } from "@/features/projects/api";
import { getScenarios } from "@/features/scenarios/api";
import { ScenariosPage } from "@/features/scenarios/components/scenarios-page";
import { can } from "@/lib/permissions";

export default async function ScenariosRoute(props: PageProps<"/projects/[projectId]/scenarios">) {
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

  return <ScenariosPage project={project} access={access} scenarios={await getScenarios(projectId)} />;
}
