import { Lock, RefreshCw, Trash2, Webhook as WebhookIcon } from "lucide-react";
import Link from "next/link";

import { ConfirmAction } from "@/components/confirm-action";
import { EmptyState } from "@/components/states";
import { Time } from "@/components/time";
import { CopyButton } from "@/features/environments/components/copy-button";
import type { Project } from "@/features/projects/types";

import { createWebhook, deleteWebhook, rotateWebhook } from "../actions";
import type { Webhook } from "../types";
import { SecretDialog } from "./secret-dialog";

// WebhookPage is how a tool that is neither GitHub nor GitLab starts a run: the address
// it sends signed requests to, and the secret it signs them with. For the project's
// administrators; to anyone else it says so and shows nothing, since the API would
// refuse every call anyway.
export function WebhookPage({
  project,
  webhook,
  allowed,
  now,
}: {
  project: Project;
  // Null when the project has none, or when the caller may not read it.
  webhook: Webhook | null;
  allowed: boolean;
  now: Date;
}) {
  return (
    <div className="flex flex-col gap-8">
      <header className="flex flex-wrap items-start justify-between gap-4">
        <div className="flex flex-col gap-1">
          <Link href={`/projects/${project.id}`} className="text-xs text-muted-foreground hover:underline">
            {project.name}
          </Link>
          <h1 className="text-3xl font-normal tracking-tight">Webhook</h1>
          <p className="max-w-xl text-sm text-muted-foreground">
            For a tool that is neither GitHub nor GitLab, such as a deploy script or Jenkins: it signs a request with a
            secret and Ballast starts a run in {project.name}, through the same guardrails as any other run.{" "}
            <Link href="/docs/webhook" className="text-brand hover:underline">
              How to sign a request
            </Link>
            .
          </p>
        </div>
        {allowed && !webhook && (
          <SecretDialog
            label="Create webhook"
            icon={<WebhookIcon aria-hidden />}
            title="Create the webhook"
            intro="Ballast makes a signing secret and shows it once. A project has one webhook."
            submit="Create webhook"
            busy="Creating…"
            act={createWebhook.bind(null, project.id)}
          />
        )}
      </header>

      {!allowed ? (
        <p className="flex items-center gap-2 text-sm text-muted-foreground">
          <Lock aria-hidden className="size-4" />
          Only a project administrator manages the webhook.
        </p>
      ) : webhook ? (
        <WebhookCard projectId={project.id} webhook={webhook} now={now} />
      ) : (
        <EmptyState
          icon={WebhookIcon}
          title="No webhook yet"
          description="Create one to start runs from a tool that can sign a request."
        />
      )}
    </div>
  );
}

// WebhookCard is the webhook that exists: where to send, and when its secret was made
// and last replaced. The secret is not here and cannot be.
export function WebhookCard({ projectId, webhook, now }: { projectId: string; webhook: Webhook; now: Date }) {
  return (
    <section aria-label="Webhook" className="flex flex-col gap-5 rounded-xl border bg-card p-5">
      <div className="flex flex-col gap-1.5">
        <h2 className="text-[11px] tracking-wider text-muted-foreground uppercase">Send signed requests to</h2>
        <div className="flex items-center gap-2 rounded-md border bg-background p-2">
          <code className="data min-w-0 flex-1 truncate text-xs">POST {webhook.path}</code>
          <CopyButton value={webhook.path} label="address" />
        </div>
        <p className="text-xs text-muted-foreground">
          After the address of your Ballast API, the one you give the command as <span className="data">BALLAST_URL</span>.
        </p>
      </div>

      <dl className="grid gap-4 text-sm sm:grid-cols-2">
        <div className="flex flex-col gap-0.5">
          <dt className="text-[11px] tracking-wider text-muted-foreground uppercase">Created</dt>
          <dd className="data text-[12.5px]">
            <Time iso={webhook.created_at} mode="relative" now={now} />
          </dd>
        </div>
        <div className="flex flex-col gap-0.5">
          <dt className="text-[11px] tracking-wider text-muted-foreground uppercase">Secret last replaced</dt>
          <dd className="data text-[12.5px]">
            {webhook.rotated_at ? <Time iso={webhook.rotated_at} mode="relative" now={now} /> : "never"}
          </dd>
        </div>
      </dl>

      <div className="flex flex-wrap items-start gap-3">
        <SecretDialog
          label="Replace secret"
          icon={<RefreshCw aria-hidden />}
          variant="outline"
          title="Replace the secret"
          intro="The current secret stops working at once, and the address does not change. Update the tool that signs the requests with the new one."
          submit="Replace secret"
          busy="Replacing…"
          act={rotateWebhook.bind(null, projectId)}
        />
        <ConfirmAction
          icon={<Trash2 aria-hidden />}
          label="Remove webhook"
          variant="outline"
          question="Remove the webhook? The tool that signs requests is refused from its next one. The runs it started stay in the history."
          confirm="Remove webhook"
          busy="Removing…"
          act={deleteWebhook.bind(null, projectId)}
        />
      </div>
    </section>
  );
}
