"use client";

import { useState } from "react";

import { Dock, type ProfileDraft } from "@/features/builder/components/dock";
import { BuilderHelp } from "@/features/builder/components/builder-help";
import { Palette } from "@/features/builder/components/palette";
import { PathField } from "@/features/builder/components/path-field";
import type { CatalogEndpoint } from "@/features/endpoints/types";
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
const thresholds = [
  { id: "t1", metric: "http_req_duration", expression: "p(95)<300" },
  { id: "t2", metric: "http_req_failed", expression: "rate<0.01" },
];

const staging = { id: "e1", name: "staging-eu", effectiveVus: 100, effectiveSeconds: 600 };

function DockSample({ draft, canEdit, dirty, target }: { draft: ProfileDraft; canEdit: boolean; dirty: boolean; target: typeof staging | undefined }) {
  return (
    <Dock
      endpoints={[
        { name: "POST /login", usable: true },
        { name: "GET /users/{id}", usable: false },
      ]}
      projectId="checkout"
      scenarioId="g1"
      thresholds={thresholds}
      canEdit={canEdit}
      draft={draft}
      onDraft={() => {}}
      dirty={dirty}
      hasOwnParams
      target={target}
    />
  );
}

const catalog: CatalogEndpoint[] = [
  {
    id: "1",
    source: "Test target",
    method: "POST",
    path: "/auth/login",
    tag: "Auth",
    summary: "Log in with the single test user",
    path_params: [],
    headers: {},
    content_type: "application/json",
    body: '{"email":"{{email}}"}',
    body_too_long: false,
    created_at: "",
  },
  {
    id: "2",
    source: "Test target",
    method: "POST",
    path: "/echo",
    tag: "Load testing",
    summary: "Echo the JSON body",
    path_params: [],
    headers: {},
    content_type: "application/json",
    body: "",
    body_too_long: false,
    created_at: "",
  },
  {
    id: "3",
    source: "Test target",
    method: "GET",
    path: "/products/{id}",
    tag: "Catalog",
    summary: "One product",
    path_params: ["id"],
    headers: {},
    content_type: "",
    body: "",
    body_too_long: false,
    created_at: "",
  },
];

function PathSample() {
  const [path, setPath] = useState("/");
  return (
    <div className="flex max-w-md items-center rounded-md border bg-background pl-2 text-xs focus-within:ring-2 focus-within:ring-ring">
      <span aria-hidden className="data shrink-0 text-muted-foreground">
        $baseUrl
      </span>
      <PathField id="styleguide-path" value={path} endpoints={catalog} onChange={setPath} onPick={(e) => setPath(e.path)} />
    </div>
  );
}

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
        <p className="text-xs text-muted-foreground">Path of a request · the catalog is offered as it is typed (click the field).</p>
        <PathSample />
        <p className="text-xs text-muted-foreground">The “?” over the canvas: how the builder works, and its shortcuts.</p>
        <div className="h-16">
          <BuilderHelp />
        </div>
      </div>

      <div className="flex max-w-xs flex-col gap-2">
        <p className="text-xs text-muted-foreground">Palette · each kind in its accent, always with its icon and its name.</p>
        <Palette onAdd={() => {}} />
      </div>

      <div className="flex flex-col gap-2">
        <p className="text-xs text-muted-foreground">Problems, after the server looked the graph over.</p>
        <ProblemsPanel problems={problems} checked stale={false} onSelect={() => {}} />
        <p className="text-xs text-muted-foreground">Nothing wrong, and not checked yet.</p>
        <ProblemsPanel problems={[]} checked={false} stale={false} onSelect={() => {}} />
        <p className="text-xs text-muted-foreground">The graph changed since the last check.</p>
        <ProblemsPanel problems={problems.slice(0, 2)} checked stale onSelect={() => {}} />
      </div>

      <div className="flex flex-col gap-2">
        <p className="text-xs text-muted-foreground">Dock · a profile inside the ceilings of the environment.</p>
        <DockSample
          draft={{ kind: "load", params: { target_vus: 50, ramp_up_s: 30, plateau_s: 120, ramp_down_s: 30 } }}
          canEdit
          dirty={false}
          target={staging}
        />
        <p className="text-xs text-muted-foreground">Dock · above the ceilings: the profile is lowered, and it says so. Unsaved.</p>
        <DockSample
          draft={{ kind: "stress", params: { target_vus: 400, ramp_up_s: 120, plateau_s: 600, ramp_down_s: 120 } }}
          canEdit
          dirty
          target={staging}
        />
        <p className="text-xs text-muted-foreground">Dock · read only, no environment picked.</p>
        <DockSample
          draft={{ kind: "smoke", params: { target_vus: 1, ramp_up_s: 0, plateau_s: 60, ramp_down_s: 0 } }}
          canEdit={false}
          dirty={false}
          target={undefined}
        />
        <p className="text-xs text-muted-foreground">Dock · parameters not finished.</p>
        <DockSample draft={{ kind: "load", params: { target_vus: 10, ramp_up_s: undefined, plateau_s: 60 } }} canEdit dirty target={staging} />
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
