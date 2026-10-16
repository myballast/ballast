import { CircleAlert, Info } from "lucide-react";

import { adviceFor } from "../findings";
import type { ScriptFinding } from "../types";

// ScriptFindings lists what inspecting the script found, next to the script.
// Errors refused it; warnings let it through. Neither is a run's status, so
// neither is coloured: the icon and the words say which is which.
export function ScriptFindings({ findings, kind }: { findings: ScriptFinding[]; kind: "error" | "warning" }) {
  if (findings.length === 0) return null;
  const Icon = kind === "error" ? CircleAlert : Info;

  return (
    <div role={kind === "error" ? "alert" : "status"} className="flex flex-col gap-2 rounded-lg border bg-card p-3 text-sm">
      <p className="font-medium">
        {kind === "error"
          ? "The script was not saved. Fix this and save again:"
          : "The script runs, with something to fix before you rely on its numbers:"}
      </p>
      <ul className="flex flex-col gap-3">
        {findings.map((finding, i) => (
          <li key={`${finding.code}-${finding.line ?? 0}-${i}`} className="flex gap-2">
            <Icon aria-hidden className="mt-0.5 size-4 shrink-0 text-muted-foreground" />
            <span className="flex min-w-0 flex-col gap-1">
              <span>
                {finding.line !== undefined && (
                  <span className="data mr-2 text-xs text-muted-foreground">line {finding.line}</span>
                )}
                {finding.message}
              </span>
              {finding.detail && <code className="data text-xs break-all text-muted-foreground">{finding.detail}</code>}
              {adviceFor(finding) && (
                <span className="text-muted-foreground">
                  <WithCode text={adviceFor(finding)!} />
                </span>
              )}
            </span>
          </li>
        ))}
      </ul>
    </div>
  );
}

// WithCode sets what the advice puts between backticks in the monospace face.
function WithCode({ text }: { text: string }) {
  return text.split("`").map((part, i) =>
    i % 2 === 1 ? (
      <code key={i} className="data text-xs text-foreground">
        {part}
      </code>
    ) : (
      part
    ),
  );
}
