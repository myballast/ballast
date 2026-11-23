import { Lock } from "lucide-react";
import Link from "next/link";

import type { Project } from "@/features/projects/types";

import { creditsLeft, liveTokens } from "../tokens";
import type { ApiTokenList } from "../types";
import { CreateToken } from "./create-token";
import { TokenList } from "./token-list";

// TokensPage is the credentials a project's pipelines hold: who made each, when it
// was last used, when it stops. For the project's administrators; to anyone else
// it says so and shows nothing, since the API would refuse every call anyway.
export function TokensPage({
  project,
  tokens,
  now,
}: {
  project: Project;
  // Null for a caller who may not manage the project.
  tokens: ApiTokenList | null;
  now: Date;
}) {
  return (
    <div className="flex flex-col gap-8">
      <header className="flex flex-wrap items-start justify-between gap-4">
        <div className="flex flex-col gap-1">
          <Link href={`/projects/${project.id}`} className="text-xs text-muted-foreground hover:underline">
            {project.name}
          </Link>
          <h1 className="text-3xl font-normal tracking-tight">CI tokens</h1>
          <p className="max-w-xl text-sm text-muted-foreground">
            What a pipeline uses to run tests in {project.name} without a person signed in. A token reads this project
            and launches runs in it, and nothing else.
          </p>
          {tokens && (
            <p className="data text-xs text-muted-foreground">
              <span className="num">{liveTokens(tokens.tokens)}</span> of <span className="num">{tokens.limit}</span> in
              use
            </p>
          )}
        </div>
        {tokens && (
          <CreateToken
            projectId={project.id}
            full={creditsLeft(tokens.tokens, tokens.limit) === 0}
            limit={tokens.limit}
          />
        )}
      </header>

      {tokens ? (
        <TokenList projectId={project.id} tokens={tokens.tokens} now={now} />
      ) : (
        <p className="flex items-center gap-2 text-sm text-muted-foreground">
          <Lock aria-hidden className="size-4" />
          Only a project administrator manages CI tokens.
        </p>
      )}
    </div>
  );
}
