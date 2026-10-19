import { Globe } from "lucide-react";
import Link from "next/link";

import { EmptyState } from "@/components/states";

import type { Environment } from "../types";
import { describeWindow } from "../window";
import { formatCap } from "./ceilings";
import { VerificationStatus } from "./verification-status";
import { VerifyDialog } from "./verify-dialog";

const columns =
  "grid grid-cols-[9rem_minmax(0,1fr)_12rem_5.5rem_6.5rem_13rem_4.5rem] items-center gap-3 px-4";

// EnvironmentList is the targets of a project as the design lays them out: name,
// base URL, how far the verification has gone, the two ceilings, the hours it
// takes load, and the one thing to do next — Edit once verified, Verify before.
//
// Verify opens the dialog for whoever manages environments. Anybody else, who
// cannot read the challenge, is taken to the environment instead: the table
// never offers an action the API would refuse.
export function EnvironmentList({
  projectId,
  environments,
  canManage = false,
  now,
  action,
}: {
  projectId: string;
  environments: Environment[];
  canManage?: boolean;
  now: Date;
  // What the empty state offers, if the caller may declare one.
  action?: React.ReactNode;
}) {
  if (environments.length === 0) {
    return (
      <EmptyState
        icon={Globe}
        title="No environment yet"
        description="An environment is a target the project may send load to, once its ownership is verified and an admin has approved it."
        action={action}
      />
    );
  }

  return (
    <div className="relative overflow-x-auto rounded-xl border bg-card">
      <div role="table" aria-label="Environments" className="min-w-[62rem]">
        <div role="row" className={`${columns} border-b py-2 text-[11px] tracking-wider text-muted-foreground uppercase`}>
          <div role="columnheader">Name</div>
          <div role="columnheader">Base URL</div>
          <div role="columnheader">Verification</div>
          <div role="columnheader" className="text-right">
            Max VUs
          </div>
          <div role="columnheader" className="text-right">
            Max duration
          </div>
          <div role="columnheader">Allowed window</div>
          <div role="columnheader">
            <span className="sr-only">Action</span>
          </div>
        </div>

        {environments.map((e) => (
          <div key={e.id} role="row" className={`${columns} min-h-10 border-b py-1.5 text-sm last:border-b-0 hover:bg-muted/30`}>
            <div role="cell" className="truncate text-[12.5px]">
              <Link href={`/projects/${projectId}/environments/${e.id}`} className="hover:underline">
                {e.name}
              </Link>
            </div>
            <div role="cell" className="data truncate text-[11.5px] text-muted-foreground" title={e.base_url}>
              {e.base_url}
            </div>
            <div role="cell">
              <VerificationStatus status={e.verification_status} />
            </div>
            <div role="cell" className="num text-right text-[11.5px]">
              {e.max_vus.toLocaleString("en-GB")}
            </div>
            <div role="cell" className="num text-right text-[11.5px]">
              {formatCap(e.max_duration_s)}
            </div>
            <div role="cell" className="data truncate text-[11.5px] text-muted-foreground" title={describeWindow(e.allowed_window)}>
              {describeWindow(e.allowed_window)}
            </div>
            <div role="cell" className="text-right text-[11.5px]">
              {e.verification_status === "verified" && canManage ? (
                <Link href={`/projects/${projectId}/environments/${e.id}/edit`} className="text-brand hover:underline">
                  Edit
                </Link>
              ) : e.verification_status !== "verified" && canManage ? (
                <VerifyDialog projectId={projectId} environment={e} now={now} />
              ) : (
                <Link href={`/projects/${projectId}/environments/${e.id}`} className="text-brand hover:underline">
                  View
                </Link>
              )}
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}
