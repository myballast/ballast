import { ProjectHeader, RestrictedProject } from "@/features/projects/components/project-overview";
import { getAccess, getProject } from "@/features/projects/api";
import { loadRunsPage } from "@/features/runs/list";
import { RunsPage } from "@/features/runs/components/runs-page";
import { can } from "@/lib/permissions";

export default async function RunsRoute(props: PageProps<"/projects/[projectId]/runs">) {
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

  const page = await loadRunsPage(projectId, await props.searchParams);
  return <RunsPage project={project} access={access} page={page} />;
}
