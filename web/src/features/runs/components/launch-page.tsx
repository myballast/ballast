import { Lock } from "lucide-react";
import Link from "next/link";

import { EmptyState } from "@/components/states";
import type { Environment } from "@/features/environments/types";
import type { Project } from "@/features/projects/types";
import type { ScenarioSummary } from "@/features/scenarios/types";
import { can, type Access } from "@/lib/permissions";

import { LaunchForm } from "./launch-form";

export function LaunchPage({
  project,
  access,
  scenarios,
  environments,
}: {
  project: Project;
  access: Access;
  scenarios: ScenarioSummary[];
  environments: Environment[];
}) {
  const verified = environments.some((e) => e.verification_status === "verified");

  return (
    <div className="flex flex-col gap-6">
      <div className="flex flex-col gap-1">
        <h1 className="text-3xl font-normal tracking-tight">Run a test</h1>
        <p className="text-sm text-muted-foreground">
          in <Link href={`/projects/${project.id}`} className="text-foreground hover:underline">{project.name}</Link>
        </p>
      </div>

      {!can(access, "run_test") ? (
        <p className="flex items-center gap-2 text-sm text-muted-foreground">
          <Lock aria-hidden className="size-4" />
          Your role in this project ({access.role}) does not include launching runs.
        </p>
      ) : scenarios.length === 0 ? (
        <EmptyState title="No scenario to run" description="Import a scenario into this project first." />
      ) : !verified ? (
        <EmptyState
          title="No verified environment"
          description="Load is only ever sent to an environment whose ownership has been verified and approved."
        />
      ) : (
        <LaunchForm projectId={project.id} scenarios={scenarios} environments={environments} />
      )}
    </div>
  );
}
