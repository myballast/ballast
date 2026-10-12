import { ProjectList } from "@/features/projects/components/project-list";
import { getProjectOverviews } from "@/features/projects/api";

export default async function ProjectsPage() {
  const overviews = await getProjectOverviews();
  return <ProjectList overviews={overviews} now={new Date()} />;
}
