import { EnvironmentDetail } from "@/features/environments/components/environment-detail";
import { loadEnvironmentPage } from "@/features/environments/detail";

export default async function EnvironmentRoute(props: PageProps<"/projects/[projectId]/environments/[environmentId]">) {
  const { projectId, environmentId } = await props.params;
  return <EnvironmentDetail page={await loadEnvironmentPage(projectId, environmentId)} />;
}
