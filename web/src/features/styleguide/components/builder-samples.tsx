"use client";

import { ProblemsPanel, type Problem } from "@/features/builder/components/problems-panel";
import { ScriptPanel, type Draft } from "@/features/builder/components/script-panel";

const problems: Problem[] = [
  { nodeId: "n3", where: "Think time", message: "The node \"n3\" is on a cycle that never ends: only a loop may go back, and it counts.", level: "error" },
  { nodeId: "n9", where: "GET /api/cart/{cartId}", message: "The node \"n9\" cannot be reached from the start.", level: "error" },
  { nodeId: "n5", where: "Loop", message: "Give the loop a maximum between 1 and 1000.", level: "error" },
  { nodeId: "n4", where: "Condition", message: "Reads token before any step before it writes it.", level: "warning" },
];

const script = [
  'import http from "k6/http";',
  'import { check, fail, sleep } from "k6";',
  "",
  "const BASE_URL = __ENV.BASE_URL;",
  "",
  "export default function () {",
  "  const vars = {};",
  "  let res = null;",
  '  res = http.request("GET", BASE_URL + "/items", null, {',
  "    headers: {},",
  '    tags: { name: "GET /items" },',
  "  });",
  "  return;",
  "}",
  "",
].join("\n");

// The two panels of the builder's checking, in each state they have. A page of the
// reference is a server component and cannot hand them the functions they take, so
// this is the client side of that.
export function BuilderSamples() {
  const drafts: { label: string; draft: Draft }[] = [
    { label: "Nothing changed since the last save", draft: { kind: "same" } },
    { label: "Changed, not written yet", draft: { kind: "unchecked" } },
    { label: "The graph has errors", draft: { kind: "errors", count: 3 } },
    { label: "Written", draft: { kind: "ready", script } },
  ];

  return (
    <div className="flex flex-col gap-6">
      <div className="flex flex-col gap-2">
        <p className="text-xs text-muted-foreground">Problems, after the server looked the graph over.</p>
        <ProblemsPanel problems={problems} checked stale={false} onSelect={() => {}} />
        <p className="text-xs text-muted-foreground">Nothing wrong, and not checked yet.</p>
        <ProblemsPanel problems={[]} checked={false} stale={false} onSelect={() => {}} />
        <p className="text-xs text-muted-foreground">The graph changed since the last check.</p>
        <ProblemsPanel problems={problems.slice(0, 2)} checked stale onSelect={() => {}} />
      </div>

      {drafts.map(({ label, draft }) => (
        <div key={label} className="flex flex-col gap-2">
          <p className="text-xs text-muted-foreground">Script preview · {label}</p>
          <ScriptPanel saved={script} version={4} draft={draft} onWrite={() => {}} pending={false} canWrite />
        </div>
      ))}
    </div>
  );
}
