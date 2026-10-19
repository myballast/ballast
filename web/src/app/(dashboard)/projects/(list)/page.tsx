import { ProjectList } from "@/features/projects/components/project-list";
import { getProjectOverviews } from "@/features/projects/api";
import { getPreferences } from "@/features/projects/preferences";

export default async function ProjectsPage() {
  const [overviews, { sort, view }] = await Promise.all([getProjectOverviews(), getPreferences()]);
  return <ProjectList overviews={overviews} now={new Date()} sort={sort} view={view} />;
}
