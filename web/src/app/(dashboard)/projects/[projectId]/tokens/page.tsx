import { getApiTokens } from "@/features/api-tokens/api";
import { TokensPage } from "@/features/api-tokens/components/tokens-page";
import { getAccess, getProject } from "@/features/projects/api";
import { can } from "@/lib/permissions";

export default async function TokensRoute(props: PageProps<"/projects/[projectId]/tokens">) {
  const { projectId } = await props.params;
  const [project, access] = await Promise.all([getProject(projectId), getAccess(projectId)]);

  // Only an administrator may read the tokens; asking anyone else would be refused.
  const tokens = can(access, "manage_project") ? await getApiTokens(projectId) : null;

  return <TokensPage project={project} tokens={tokens} now={new Date()} />;
}
