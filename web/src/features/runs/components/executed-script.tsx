import { FileCode } from "lucide-react";

import type { ScenarioVersion } from "@/features/scenarios/types";

// ExecutedScript is the script this run executed: the scenario version it
// names, which never changes, and not the scenario as it is today.
export function ExecutedScript({ version }: { version: ScenarioVersion | null }) {
  if (!version) {
    return (
      <p className="flex items-center gap-2 text-sm text-muted-foreground">
        <FileCode aria-hidden className="size-4" />
        The script this run executed cannot be read.
      </p>
    );
  }

  return (
    <details className="rounded-xl border">
      <summary className="flex cursor-pointer items-center gap-2 px-4 py-3 text-sm">
        <FileCode aria-hidden className="size-4 text-muted-foreground" />
        Script executed <span className="data text-xs text-muted-foreground">v{version.version}</span>
      </summary>
      <pre className="data max-h-96 overflow-auto border-t p-4 text-xs leading-5">{version.script}</pre>
    </details>
  );
}
