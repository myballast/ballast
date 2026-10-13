import { ProjectHeader, ProjectOverview, RestrictedProject } from "@/features/projects/components/project-overview";
import { loadProjectPage } from "@/features/projects/overview";

export default async function ProjectPage(props: PageProps<"/projects/[projectId]">) {
  const { projectId } = await props.params;
  const page = await loadProjectPage(projectId);

  if (page.restricted) {
    return (
      <div className="flex flex-col gap-6">
        <ProjectHeader project={page.project} access={page.access} />
        <RestrictedProject />
      </div>
    );
  }

  return <ProjectOverview {...page} />;
}
