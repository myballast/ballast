import Link from "next/link";

import { buttonVariants } from "@/components/ui/button";

import type { DetectedSecret, ImportFinding } from "../types";
import { ImportReport } from "./import-report";
import { SecretsPanel } from "./secrets-panel";

type Target = { id: string; name: string };

// What an import leaves on screen: the report, the secrets to create, and where
// to go next. The person reads it before leaving, because the secrets are typed here.
export type Done = {
  scenarioId?: string;
  catalogCount?: number;
  secrets: DetectedSecret[];
  findings: ImportFinding[];
};

// ImportDone is what is left once the import has run: where the scenario and the
// endpoints went, the report, and the secrets to create.
export function ImportDone({
  projectId,
  done,
  environments,
  canManage,
}: {
  projectId: string;
  done: Done;
  environments: Target[];
  canManage: boolean;
}) {
  return (
    <div className="flex flex-col gap-6">
      <section className="flex flex-col gap-2 rounded-xl border p-4 text-sm">
        <h2 className="font-medium">Imported</h2>
        {done.scenarioId && <p className="text-muted-foreground">The scenario was created.</p>}
        {done.catalogCount !== undefined && (
          <p className="text-muted-foreground">
            <span className="num">{done.catalogCount}</span> endpoints are in the project&apos;s catalog.
          </p>
        )}
        <div className="flex flex-wrap gap-2">
          {done.scenarioId && (
            <Link href={`/projects/${projectId}/scenarios/${done.scenarioId}`} className={buttonVariants()}>
              Open the scenario
            </Link>
          )}
          {done.catalogCount !== undefined && (
            <Link href={`/projects/${projectId}/endpoints`} className={buttonVariants({ variant: "outline" })}>
              See the endpoints
            </Link>
          )}
        </div>
      </section>

      {done.secrets.length > 0 && (
        <SecretsPanel projectId={projectId} secrets={done.secrets} environments={environments} canManage={canManage} />
      )}

      <ImportReport findings={done.findings} />
    </div>
  );
}
