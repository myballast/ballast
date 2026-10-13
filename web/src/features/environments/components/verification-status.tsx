import { ShieldAlert, ShieldCheck, ShieldQuestion, ShieldX } from "lucide-react";

import { cn } from "@/lib/utils";

import type { VerificationStatus as Status } from "../types";

// An environment's verification is not the status of a run or a threshold:
// it is said with an icon and a word, in neutral text, never in green or amber.
const verification: Record<Status, { label: string; icon: typeof ShieldCheck }> = {
  verified: { label: "Verified", icon: ShieldCheck },
  proof_ok: { label: "Awaiting approval", icon: ShieldQuestion },
  pending: { label: "Not verified", icon: ShieldAlert },
  verification_failed: { label: "Verification failed", icon: ShieldX },
};

export function verificationLabel(status: Status): string {
  return verification[status].label;
}

export function VerificationStatus({ status, className }: { status: Status; className?: string }) {
  const { label, icon: Icon } = verification[status];
  return (
    <span className={cn("flex items-center gap-1 text-xs text-muted-foreground", className)}>
      <Icon aria-hidden className="size-3.5 shrink-0" />
      {label}
    </span>
  );
}
