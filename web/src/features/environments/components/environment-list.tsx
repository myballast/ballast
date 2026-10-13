import { ChevronRight, Globe } from "lucide-react";
import Link from "next/link";

import { EmptyState } from "@/components/states";

import type { Environment } from "../types";
import { Ceilings } from "./ceilings";
import { VerificationStatus } from "./verification-status";

export function EnvironmentList({
  projectId,
  environments,
  action,
}: {
  projectId: string;
  environments: Environment[];
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
    <ul className="flex flex-col divide-y rounded-xl border">
      {environments.map((e) => (
        <li key={e.id}>
          <Link
            href={`/projects/${projectId}/environments/${e.id}`}
            className="flex items-center gap-3 px-4 py-3 text-sm hover:bg-muted/40"
          >
            <span className="flex min-w-0 flex-1 flex-col gap-1.5">
              <span className="flex flex-wrap items-center gap-x-3 gap-y-1">
                <span className="font-medium">{e.name}</span>
                <VerificationStatus status={e.verification_status} />
              </span>
              <span className="data truncate text-xs text-muted-foreground">{e.base_url}</span>
              <Ceilings environment={e} />
            </span>
            <ChevronRight aria-hidden className="size-4 shrink-0 text-muted-foreground" />
          </Link>
        </li>
      ))}
    </ul>
  );
}
