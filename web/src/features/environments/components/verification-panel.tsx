import { ShieldCheck, ShieldQuestion } from "lucide-react";

import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { formatDateTime } from "@/lib/format";

import { approveEnvironment } from "../actions";
import type { Environment, VerificationChallenge } from "../types";
import { ConfirmAction } from "@/components/confirm-action";
import { CopyButton } from "./copy-button";
import { VerifyButton } from "./verify-button";

function Title({ children }: { children: React.ReactNode }) {
  return (
    <CardTitle className="text-[11px] font-normal tracking-wider text-muted-foreground uppercase">
      {children}
    </CardTitle>
  );
}

function Copyable({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex flex-col gap-1.5">
      <span className="text-xs text-muted-foreground">{label}</span>
      <div className="flex items-center gap-2 rounded-md border bg-background py-1 pr-1 pl-3">
        <code className="data min-w-0 flex-1 text-xs break-all">{value}</code>
        <CopyButton value={value} label={label.toLowerCase()} />
      </div>
    </div>
  );
}

// The shell line that writes the file, for whoever serves the target from a
// directory. The path is the URL's: /.well-known sits at the root of the origin.
function command(challenge: VerificationChallenge): string {
  const file = new URL(challenge.url).pathname.replace(/^\//, "");
  return `printf '%s' '${challenge.token}' > <web root>/${file}`;
}

// VerificationPanel walks a target through its two steps: proving somebody
// owns it, then an administrator approving it. They are separate on purpose —
// whoever declares a target cannot also be the one who lets load reach it.
export function VerificationPanel({
  projectId,
  environment,
  challenge,
  canVerify,
  canApprove,
}: {
  projectId: string;
  environment: Environment;
  // Only read for whoever manages environments; the API refuses everyone else.
  challenge: VerificationChallenge | null;
  canVerify: boolean;
  canApprove: boolean;
}) {
  const status = environment.verification_status;

  if (status === "verified") {
    return (
      <Card>
        <CardHeader>
          <Title>Verification</Title>
        </CardHeader>
        <CardContent className="flex items-start gap-2 text-sm">
          <ShieldCheck
            aria-hidden
            className="mt-0.5 size-4 shrink-0 text-muted-foreground"
          />
          <p>
            Approved
            {challenge?.verified_at && (
              <>
                {" "}
                on{" "}
                <span className="num">
                  {formatDateTime(challenge.verified_at)}
                </span>
              </>
            )}
            {challenge?.verified_by && (
              <>
                {" "}
                by{" "}
                <span className="font-medium">{challenge.verified_by_name || challenge.verified_by}</span>
              </>
            )}
            . Runs of this project may send load to it, within its ceilings.
          </p>
        </CardContent>
      </Card>
    );
  }

  if (status === "proof_ok") {
    return (
      <Card>
        <CardHeader>
          <Title>Approval</Title>
        </CardHeader>
        <CardContent className="flex flex-col gap-4 text-sm">
          <p className="flex items-start gap-2">
            <ShieldQuestion
              aria-hidden
              className="mt-0.5 size-4 shrink-0 text-muted-foreground"
            />
            <span>
              The target served its token: somebody who controls it asked for it
              to be tested. No load reaches it until an admin of this project
              approves it.
            </span>
          </p>
          {canApprove ? (
            <ConfirmAction
              icon={<ShieldCheck aria-hidden />}
              label="Approve target"
              question={
                <>
                  Allow this project&apos;s runs to send load to{" "}
                  <span className="data break-all">
                    {new URL(environment.base_url).host}
                  </span>
                  ? Its host is added to the project&apos;s allowlist.
                </>
              }
              confirm="Approve target"
              busy="Approving…"
              act={approveEnvironment.bind(null, projectId, environment.id)}
            />
          ) : (
            <p className="text-muted-foreground">
              Waiting for an admin of this project to approve it.
            </p>
          )}
        </CardContent>
      </Card>
    );
  }

  return (
    <Card>
      <CardHeader>
        <Title>Verification</Title>
      </CardHeader>
      <CardContent className="flex flex-col gap-4 text-sm">
        {status === "verification_failed" && (
          <p className="text-muted-foreground">
            The last attempt did not find the token at the address below. Fix
            what the message said, then verify again.
          </p>
        )}

        {!canVerify || !challenge ? (
          <p className="text-muted-foreground">
            Somebody with the maintainer role on this project has to prove the
            target is theirs before it can be approved.
          </p>
        ) : (
          <>
            <ol className="flex list-decimal flex-col gap-5 pl-5 marker:text-muted-foreground">
              <li className="pl-1">
                <div className="flex flex-col gap-3">
                  <span>
                    Serve a file at this exact address, over the target itself,
                    whose content is the token and nothing else.
                  </span>
                  <Copyable label="Address" value={challenge.url} />
                  <Copyable label="Token" value={challenge.token} />
                  <Copyable label="Command" value={command(challenge)} />
                </div>
              </li>
              <li className="pl-1">
                <div className="flex flex-col gap-3">
                  <span>
                    Ask Ballast to fetch it. A target that serves the token is
                    not yet approved: an admin of this project decides that
                    next.
                  </span>
                  <VerifyButton
                    projectId={projectId}
                    environmentId={environment.id}
                    again={status === "verification_failed"}
                  />
                </div>
              </li>
            </ol>
          </>
        )}
      </CardContent>
    </Card>
  );
}
