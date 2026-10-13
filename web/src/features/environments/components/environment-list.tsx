import { Clock, Globe, ShieldAlert, ShieldCheck } from "lucide-react";

import { EmptyState } from "@/components/states";

import type { Environment, VerificationStatus } from "../types";

// An environment's verification is not the status of a run or a threshold:
// it is said with an icon and a word, in neutral text, never in green or amber.
const verification: Record<VerificationStatus, { label: string; verified: boolean }> = {
  verified: { label: "Verified", verified: true },
  proof_ok: { label: "Awaiting approval", verified: false },
  pending: { label: "Not verified", verified: false },
  verification_failed: { label: "Verification failed", verified: false },
};

export function verificationLabel(status: VerificationStatus): string {
  return verification[status].label;
}

function minutes(seconds: number): string {
  return seconds % 60 === 0 ? `${seconds / 60} min` : `${seconds} s`;
}

export function EnvironmentList({ environments }: { environments: Environment[] }) {
  if (environments.length === 0) {
    return (
      <EmptyState
        icon={Globe}
        title="No environment yet"
        description="An environment is a target the project may send load to, once its ownership is verified."
      />
    );
  }

  return (
    <ul className="flex flex-col divide-y rounded-xl border">
      {environments.map((e) => {
        const v = verification[e.verification_status];
        const Icon = v.verified ? ShieldCheck : ShieldAlert;
        return (
          <li key={e.id} className="flex flex-col gap-1.5 px-4 py-3 text-sm">
            <div className="flex flex-wrap items-center gap-x-3 gap-y-1">
              <span className="font-medium">{e.name}</span>
              <span className="flex items-center gap-1 text-xs text-muted-foreground">
                <Icon aria-hidden className="size-3.5" />
                {v.label}
              </span>
            </div>
            <span className="data truncate text-xs text-muted-foreground">{e.base_url}</span>
            <span className="flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-muted-foreground">
              <span>
                up to <span className="num">{e.max_vus}</span> VUs for <span className="num">{minutes(e.max_duration_s)}</span>
              </span>
              {e.allowed_window && (
                <span className="flex items-center gap-1">
                  <Clock aria-hidden className="size-3.5" />
                  <span className="num">
                    {e.allowed_window.start}–{e.allowed_window.end}
                  </span>{" "}
                  {e.allowed_window.timezone}
                </span>
              )}
            </span>
          </li>
        );
      })}
    </ul>
  );
}
