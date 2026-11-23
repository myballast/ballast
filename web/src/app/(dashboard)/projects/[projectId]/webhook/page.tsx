import { getAccess, getProject } from "@/features/projects/api";
import { WebhookPage } from "@/features/webhooks/components/webhook-page";
import { getWebhook } from "@/features/webhooks/api";
import { can } from "@/lib/permissions";

export default async function WebhookRoute(props: PageProps<"/projects/[projectId]/webhook">) {
  const { projectId } = await props.params;
  const [project, access] = await Promise.all([getProject(projectId), getAccess(projectId)]);

  // Only an administrator may read the webhook; asking anyone else would be refused.
  const allowed = can(access, "manage_project");
  const webhook = allowed ? await getWebhook(projectId) : null;

  return <WebhookPage project={project} webhook={webhook} allowed={allowed} now={new Date()} />;
}
