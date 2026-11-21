import { Ban, Clock, KeyRound, ShieldCheck, Undo2 } from "lucide-react";

import { ConfirmAction } from "@/components/confirm-action";
import { EmptyState } from "@/components/states";
import { Time } from "@/components/time";
import { Badge } from "@/components/ui/badge";

import { revokeApiToken } from "../actions";
import { whoMade } from "../tokens";
import type { ApiToken, ApiTokenState } from "../types";

const columns = "grid grid-cols-[minmax(0,1.4fr)_minmax(0,1.2fr)_8rem_8rem_7rem_6rem] items-center gap-3 px-4";

// The state of a token, in words with an icon. Neutral on purpose: green, amber
// and red say how a run went, and a token that is "expired" has not failed.
const states: Record<ApiTokenState, { label: string; icon: typeof ShieldCheck }> = {
  active: { label: "Active", icon: ShieldCheck },
  expired: { label: "Expired", icon: Clock },
  revoked: { label: "Revoked", icon: Ban },
};

// TokenList is the project's tokens: what each is called, who made it, when it was
// last used and when it stops working. The secret is not here and cannot be: it was
// shown once, when the token was created, and only its hash is kept.
//
// A revoked token stays in the list, after the others and set apart, so that the
// history of what was withdrawn is still readable. Revoking is not red: the danger
// is said by the words and the question.
export function TokenList({ projectId, tokens, now }: { projectId: string; tokens: ApiToken[]; now: Date }) {
  if (tokens.length === 0) {
    return (
      <EmptyState
        icon={KeyRound}
        title="No CI tokens yet"
        description="Create one to run a scenario from GitHub Actions, GitLab CI or any other pipeline."
      />
    );
  }

  return (
    <div className="relative overflow-x-auto rounded-xl border bg-card">
      <div role="table" aria-label="CI tokens" className="min-w-[56rem]">
        <div role="row" className={`${columns} border-b py-2 text-[11px] tracking-wider text-muted-foreground uppercase`}>
          <div role="columnheader">Token</div>
          <div role="columnheader">Created</div>
          <div role="columnheader">Last used</div>
          <div role="columnheader">Expires</div>
          <div role="columnheader">State</div>
          <div role="columnheader">
            <span className="sr-only">Actions</span>
          </div>
        </div>

        {tokens.map((token) => {
          const { label, icon: Icon } = states[token.state];
          const revoked = token.state === "revoked";

          return (
            <div
              key={token.id}
              role="row"
              className={`${columns} min-h-12 border-b py-2 text-sm last:border-b-0 hover:bg-muted/30 ${
                revoked ? "text-muted-foreground" : ""
              }`}
            >
              <div role="cell" className="flex min-w-0 flex-col">
                <span className="truncate text-[12.5px]">{token.name}</span>
                <span className="data truncate text-[11px] text-muted-foreground">{token.prefix}…</span>
              </div>
              <div role="cell" className="flex min-w-0 flex-col text-[11.5px]">
                <span className="truncate">{whoMade(token)}</span>
                <span className="data text-muted-foreground">
                  <Time iso={token.created_at} mode="relative" now={now} />
                </span>
              </div>
              <div role="cell" className="data text-[11.5px] text-muted-foreground">
                {token.last_used_at ? <Time iso={token.last_used_at} mode="relative" now={now} /> : "never used"}
              </div>
              <div role="cell" className="data text-[11.5px] text-muted-foreground">
                {revoked && token.revoked_at ? (
                  <>
                    revoked <Time iso={token.revoked_at} mode="relative" now={now} />
                  </>
                ) : (
                  <Time iso={token.expires_at} mode="moment" />
                )}
              </div>
              <div role="cell">
                <Badge variant="outline" className="text-muted-foreground">
                  <Icon aria-hidden />
                  {label}
                </Badge>
              </div>
              <div role="cell" className="flex justify-end">
                {token.state === "active" && (
                  <ConfirmAction
                    icon={<Undo2 aria-hidden />}
                    label="Revoke"
                    variant="outline"
                    question={
                      <>
                        Revoke <span className="data">{token.name}</span>? Any pipeline that uses it stops working on its
                        next request.
                      </>
                    }
                    confirm="Revoke token"
                    busy="Revoking…"
                    act={revokeApiToken.bind(null, projectId, token.id)}
                  />
                )}
              </div>
            </div>
          );
        })}
      </div>
    </div>
  );
}
