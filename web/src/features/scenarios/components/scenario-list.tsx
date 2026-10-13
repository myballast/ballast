import { FileCode } from "lucide-react";

import { EmptyState } from "@/components/states";

import type { ScenarioSummary } from "../types";

export function ScenarioList({ scenarios }: { scenarios: ScenarioSummary[] }) {
  if (scenarios.length === 0) {
    return (
      <EmptyState
        icon={FileCode}
        title="No scenario yet"
        description="A scenario is a k6 script. Import one through the API to run it here."
      />
    );
  }

  return (
    <ul className="flex flex-col divide-y rounded-xl border">
      {scenarios.map((s) => (
        <li key={s.id} className="flex flex-wrap items-center gap-x-3 gap-y-1 px-4 py-3 text-sm">
          <span className="font-medium">{s.name}</span>
          <span className="data text-xs text-muted-foreground">v{s.version}</span>
          <span className="text-xs text-muted-foreground">default profile {s.default_profile}</span>
        </li>
      ))}
    </ul>
  );
}
