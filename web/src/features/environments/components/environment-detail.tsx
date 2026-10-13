import { Pencil, Trash2 } from "lucide-react";
import Link from "next/link";

import { buttonVariants } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { formatDateTime } from "@/lib/format";
import { can } from "@/lib/permissions";

import { deleteEnvironment } from "../actions";
import type { EnvironmentPage } from "../detail";
import { ConfirmAction } from "./confirm-action";
import { Ceilings } from "./ceilings";
import { SecretList } from "./secret-list";
import { VerificationPanel } from "./verification-panel";
import { VerificationStatus } from "./verification-status";

// EnvironmentDetail is one target: where it is, what it may take, how far its
// verification has gone, and the secrets runs against it receive.
export function EnvironmentDetail({ page }: { page: EnvironmentPage }) {
  const { project, access, environment, challenge, secrets } = page;
  const manage = can(access, "manage_environments");

  return (
    <div className="flex flex-col gap-8">
      <header className="flex flex-col gap-4">
        <Link href={`/projects/${project.id}/environments`} className="text-xs text-muted-foreground hover:underline">
          {project.name} · Environments
        </Link>
        <div className="flex flex-wrap items-start justify-between gap-4">
          <div className="flex min-w-0 flex-col gap-2">
            <div className="flex flex-wrap items-center gap-3">
              <h1 className="text-3xl font-normal tracking-tight">{environment.name}</h1>
              <VerificationStatus status={environment.verification_status} className="text-sm" />
            </div>
            <span className="data text-sm break-all text-muted-foreground">{environment.base_url}</span>
            <Ceilings environment={environment} />
            <span className="text-xs text-muted-foreground">
              declared <span className="num">{formatDateTime(environment.created_at)}</span>
            </span>
          </div>
          {/* Hidden from whoever cannot use them; the API refuses them anyway. */}
          {manage && (
            <div className="flex flex-wrap items-start gap-2">
              <Link
                href={`/projects/${project.id}/environments/${environment.id}/edit`}
                className={buttonVariants({ variant: "outline" })}
              >
                <Pencil aria-hidden />
                Edit
              </Link>
              <ConfirmAction
                icon={<Trash2 aria-hidden />}
                label="Delete environment"
                variant="destructive"
                question={
                  <>
                    Delete <span className="font-medium">{environment.name}</span>? Its secrets go with it. An
                    environment that holds runs cannot be deleted: their history would go too.
                  </>
                }
                confirm="Delete environment"
                keep="Keep it"
                busy="Deleting…"
                act={deleteEnvironment.bind(null, project.id, environment.id)}
              />
            </div>
          )}
        </div>
      </header>

      <VerificationPanel
        projectId={project.id}
        environment={environment}
        challenge={challenge}
        canVerify={manage}
        canApprove={can(access, "approve_target")}
      />

      {secrets && (
        <Card>
          <CardHeader>
            <CardTitle className="text-[11px] font-normal tracking-wider text-muted-foreground uppercase">
              Secrets
            </CardTitle>
          </CardHeader>
          <CardContent>
            <SecretList projectId={project.id} environmentId={environment.id} secrets={secrets} />
          </CardContent>
        </Card>
      )}
    </div>
  );
}
