import { CircleCheck, CircleX, FolderKanban, LayoutDashboard, Server, Trash2, TriangleAlert, Users, Workflow } from "lucide-react";

import { Logo } from "@/components/brand/logo";
import { Breadcrumb } from "@/components/layout/breadcrumb";
import { NavGroup, NavItem } from "@/components/layout/nav-item";
import { UserMenu } from "@/components/layout/user-menu";
import { buildTrail } from "@/features/shell/trail";
import type { ShellScope } from "@/features/shell/types";
import { MetricCard } from "@/components/metric-card";
import { BuilderSamples } from "./builder-samples";
import { ErrorStateSample } from "./error-state-sample";
import { EmptyState, LoadingState } from "@/components/states";
import {
  Skeleton,
  SkeletonCards,
  SkeletonHeading,
  SkeletonPage,
  SkeletonPanel,
  SkeletonTable,
  SkeletonTiles,
} from "@/components/skeleton";
import { Time } from "@/components/time";
import { StatusBadge } from "@/components/status-badge";
import { Button } from "@/components/ui/button";
import { OrgSelector } from "@/components/layout/org-selector";
import { InvitationCard } from "@/features/members/components/invitation-card";
import { InviteMember } from "@/features/members/components/invite-member";
import { MembersTable } from "@/features/members/components/members-table";
import { PendingInvitations } from "@/features/members/components/pending-invitations";
import { PermissionsMatrix } from "@/features/members/components/permissions-matrix";
import type { Invitation, Member, RolesMatrix } from "@/features/members/types";
import { EnvironmentList } from "@/features/environments/components/environment-list";
import { RunnerCard } from "@/features/runners/components/runner-card";
import type { Runner } from "@/features/runners/types";
import { SecretList } from "@/features/environments/components/secret-list";
import { Builder } from "@/features/builder/components/builder";
import { NewScenarioPage } from "@/features/scenarios/components/new-scenario-page";
import { ScenarioWorkspace } from "@/features/scenarios/components/scenario-workspace";
import { ScenarioList } from "@/features/scenarios/components/scenario-list";
import { ScriptEditor } from "@/features/scenarios/components/script-editor";
import { ScriptFindings } from "@/features/scenarios/components/script-findings";
import { ThresholdRules } from "@/features/scenarios/components/threshold-rules";
import { starterScript } from "@/features/scenarios/template";
import { VerificationPanel } from "@/features/environments/components/verification-panel";
import { ProjectCard } from "@/features/projects/components/project-card";
import { ProjectList } from "@/features/projects/components/project-list";
import { ProjectOverview as ProjectPage } from "@/features/projects/components/project-overview";
import { ProjectTabs } from "@/features/projects/components/project-tabs";
import { RecentRun } from "@/features/projects/components/recent-run";
import { StatTiles } from "@/features/projects/components/stat-tiles";
import { tilesOf, type ProjectStats, type StatsMeasure } from "@/features/projects/stats";
import { EndpointMetrics } from "@/features/runs/components/endpoint-metrics";
import { HttpPhases } from "@/features/runs/components/http-phases";
import { LogLine } from "@/features/runs/components/log-line";
import { RerunRun } from "@/features/runs/components/rerun-run";
import { RunFilterBar } from "@/features/runs/components/run-filters";
import { RunHeader } from "@/features/runs/components/run-header";
import { RunTabs } from "@/features/runs/components/run-tabs";
import type { RunContext } from "@/features/runs/detail";
import type { Run } from "@/features/runs/types";
import type { ScenarioGraph } from "@/features/scenarios/types";
import { RunsPage } from "@/features/runs/components/runs-page";
import { RunsTable } from "@/features/runs/components/runs-table";
import type { RunRow, RunsPage as RunsPageData } from "@/features/runs/list";
import type { Metric } from "@/features/runs/types";
import type { ProjectOverview } from "@/features/projects/types";
import { RunStatusBadge } from "@/features/runs/components/run-status-badge";
import type { RunStatus } from "@/features/runs/types";

const statuses: RunStatus[] = [
  "queued",
  "running",
  "passed",
  "failed",
  "errored",
  "timeout",
  "cancelled",
  "rejected",
  "skipped",
  "orphaned",
];

// Numbers of very different widths, to show that tabular figures keep the
// columns aligned digit for digit.
const latencies = [
  ["checkout", "1.69 ms", "1,214.4 req/s", "0.00 %"],
  ["search", "112.08 ms", "88.0 req/s", "12.50 %"],
  ["login", "2.11 s", "11,940.7 req/s", "0.31 %"],
];

const now = new Date("2026-10-12T18:00:00Z");

function runner(name: string, status: Runner["status"], secondsAgo: number, contract = 2): Runner {
  return {
    name,
    version: "a1b2c3d",
    contract,
    status,
    outdated: contract < 2,
    started_at: "2026-10-12T08:00:00Z",
    last_seen_at: new Date(now.getTime() - secondsAgo * 1000).toISOString(),
  };
}

const project = (
  name: string,
  slug: string,
  details: Partial<{ infra: string; region: string; owner: string; stack: string[]; favorite: boolean }> = {},
) => ({
  id: slug,
  name,
  slug,
  infra: "",
  region: "",
  owner: "",
  stack: [] as string[],
  favorite: false,
  created_at: "2026-10-01T09:00:00Z",
  ...details,
});

const environment = (name: string, verified: boolean) => ({
  id: name,
  name,
  base_url: `https://${name}.example.test`,
  verification_status: verified ? ("verified" as const) : ("pending" as const),
  max_vus: 50,
  max_duration_s: 600,
  effective_max_vus: 50,
  effective_max_duration_s: 600,
  created_at: "2026-10-01T09:00:00Z",
});

const shellScope: ShellScope = {
  project: { id: "p1", name: "storefront" },
  role: "maintainer",
  names: {
    scenarios: { s1: "checkout-flow" },
    environments: { e1: "staging" },
    run: { id: "r1", label: "checkout-flow, 15 Oct 2026, 16:05:24" },
  },
};

const shellPaths = [
  "/projects",
  "/projects/p1",
  "/projects/p1/scenarios/s1",
  "/projects/p1/environments/e1/edit",
  "/projects/p1/runs/new",
  "/runs/r1",
  // A scenario the shell was drawn before: said by its kind, never by its id.
  "/projects/p1/scenarios/3f1c0a52-7d0e-4c6a-8f55-1b2a4d9e0c11",
];

// A graph with every kind of node: two ways out of a condition and of a loop, and
// one step left unwired so that its marker shows.
const sampleGraph: ScenarioGraph = {
  start: "n1",
  nodes: [
    { id: "n1", kind: "http_request", method: "POST", path: "/api/auth/login", headers: { "Content-Type": "application/json" }, body: '{"email":"{{email}}"}' },
    { id: "n2", kind: "extract_variable", variable: "authToken", source: "body", from: "data.token" },
    { id: "n3", kind: "set_variable", variable: "email", from_env: "LOGIN_EMAIL" },
    { id: "n4", kind: "condition", expression: "code == 200" },
    { id: "n5", kind: "loop", iterations: 20, over: "items", item: "item" },
    { id: "n6", kind: "think_time", seconds: 1, seconds_max: 4 },
    { id: "n7", kind: "assertion", expression: "response_status == 200 && response_time_ms < 500", critical: true },
    { id: "n8", kind: "end" },
    { id: "n9", kind: "http_request", method: "GET", path: "/api/cart/{cartId}" },
    { id: "n10", kind: "extract_variable", variable: "requestId", source: "header", from: "X-Request-Id" },
    { id: "n11", kind: "extract_variable", variable: "code", source: "status" },
  ],
  edges: [
    { from: "n1", to: "n2" },
    { from: "n2", to: "n3" },
    { from: "n3", to: "n10" },
    { from: "n10", to: "n11" },
    { from: "n11", to: "n4" },
    { from: "n4", to: "n5", when: "true" },
    { from: "n4", to: "n8", when: "false" },
    { from: "n5", to: "n6", when: "body" },
    { from: "n5", to: "n7", when: "after" },
    { from: "n6", to: "n5" },
    { from: "n7", to: "n8" },
  ],
};

// A small catalog, for the request node's endpoint picker.
const sampleCatalog = [
  {
    id: "c1",
    source: "openapi",
    method: "POST",
    path: "/users/{user-id}/orders",
    tag: "orders",
    summary: "Create an order",
    path_params: ["user-id"],
    headers: {},
    content_type: "application/json",
    body: '{"sku":"{{sku}}","qty":1}',
    body_too_long: false,
    created_at: "2026-10-01T00:00:00Z",
  },
  {
    id: "c2",
    source: "openapi",
    method: "GET",
    path: "/products",
    tag: "catalog",
    summary: "List the products",
    path_params: [],
    headers: {},
    content_type: "",
    body: "",
    body_too_long: false,
    created_at: "2026-10-01T00:00:00Z",
  },
];

const sampleThresholds = [
  { id: "t1", metric: "http_req_duration", expression: "p(95)<300" },
  { id: "t2", metric: "http_req_failed", expression: "rate<0.01" },
];

const graphScenario = (name: string, graph: ScenarioGraph) => ({
  id: "g1",
  name,
  version: 4,
  default_profile: "load" as const,
  default_params: { target_vus: 50, ramp_up_s: 30, plateau_s: 120, ramp_down_s: 30 },
  created_at: "2026-10-01T09:00:00Z",
  updated_at: "2026-10-18T11:58:00Z",
  script: "// generated",
  mode: "graph" as const,
  graph,
  warnings: [],
});

const scenario = (name: string, version: number) => ({
  id: name,
  name,
  version,
  default_profile: "load" as const,
  default_params: { target_vus: 20, ramp_up_s: 30, plateau_s: 120, ramp_down_s: 30 },
  created_at: "2026-10-01T09:00:00Z",
  updated_at: "2026-10-12T16:20:00Z",
});

const thresholds = [
  { id: "t1", metric: "http_req_duration{name:POST /login}", expression: "p(95)<300" },
  { id: "t2", metric: "http_req_failed", expression: "rate<0.01" },
];

const runRows: RunRow[] = (
  [
    [1482, "failed", "checkout-flow", "staging", "push main", "1 284 ms", "4.80 %", "400", "8m 14s"],
    [1481, "passed", "checkout-flow", "staging", "Ada Lovelace", "842 ms", "0.90 %", "400", "8m 02s"],
    [1480, "running", "browse-and-search", "staging", "Ada Lovelace", "—", "—", "200", "—"],
    [1479, "queued", "auth-soak", "preview", "—", "—", "—", "150", "—"],
    [1478, "cancelled", "cart-stress", "staging", "Rui Runner", "—", "—", "900", "1m 03s"],
  ] as const
).map(([number, status, scenario, environment, triggeredBy, p95, errors, vus, duration]) => ({
  id: `run-${number}`,
  number,
  status,
  createdAt: "2026-10-16T12:51:19Z",
  scenario,
  environment,
  triggeredBy,
  p95,
  errors,
  vus,
  duration,
}));

const wave = (base: number, amplitude: number, phase: number) =>
  Array.from({ length: 30 }, (_, i) => base + amplitude * Math.sin(i / 3 + phase));

const measure = (value: number | null, previous: number | null, change: number | null, series: (number | null)[]): StatsMeasure => ({
  value,
  previous,
  change,
  series,
});

const sampleStats: ProjectStats = {
  days: 30,
  from: "2026-09-17T00:00:00Z",
  to: "2026-10-17T00:00:00Z",
  pass_rate: measure(0.864, 0.896, -3.2, wave(0.86, 0.06, 1)),
  failed_runs: measure(17, 13, 4, wave(0.6, 0.6, 4).map((v) => Math.max(0, Math.round(v)))),
  total_runs: measure(125, 112.6, 11, wave(4, 2, 9).map((v) => Math.round(v))),
  avg_duration_s: measure(401, 383, 18, wave(400, 60, 13)),
  p95_ms: measure(812, 716, 96, [...wave(780, 90, 21).slice(0, 12), null, null, ...wave(820, 50, 3).slice(14)]),
};

// No run at all in the window: every tile says so with a dash.
const emptyStats: ProjectStats = {
  ...sampleStats,
  pass_rate: measure(null, null, null, Array(30).fill(null)),
  failed_runs: measure(0, 0, null, Array(30).fill(0)),
  total_runs: measure(0, 0, null, Array(30).fill(0)),
  avg_duration_s: measure(null, null, null, Array(30).fill(null)),
  p95_ms: measure(null, null, null, Array(30).fill(null)),
};

// A project that started last week: figures, and nothing before to compare with.
const newStats: ProjectStats = {
  ...sampleStats,
  pass_rate: measure(1, null, null, [...Array(23).fill(null), ...Array(7).fill(1)]),
  failed_runs: measure(0, 0, null, Array(30).fill(0)),
  total_runs: measure(9, 0, null, [...Array(23).fill(0), 1, 2, 1, 0, 3, 1, 1]),
  avg_duration_s: measure(120, null, null, [...Array(23).fill(null), 110, 130, 120, null, 125, 115, 120]),
  p95_ms: measure(430, null, null, [...Array(23).fill(null), 400, 450, 420, null, 440, 430, 440]),
};

const runsPage = (overrides: Partial<RunsPageData>): RunsPageData => ({
  rows: [],
  filters: {},
  scenarios: [{ id: "s1", name: "checkout-flow" }],
  environments: [{ id: "e1", name: "staging" }],
  inHistory: false,
  ...overrides,
});

const sampleNow = new Date("2026-10-16T15:00:00Z");

const sampleRun = (status: Run["status"]): Run => ({
  id: "r1",
  number: 1482,
  triggered_by: "Ada Lovelace",
  project_id: "checkout",
  scenario_id: "s",
  environment_id: "staging",
  scenario_version: 3,
  profile: "load",
  params: { target_vus: 400, ramp_up_s: 60, plateau_s: 360, ramp_down_s: 60 },
  status,
  created_at: "2026-10-16T14:02:00Z",
  started_at: "2026-10-16T14:02:11Z",
  ended_at: status === "running" ? undefined : "2026-10-16T14:10:25Z",
  metrics: [],
  thresholds: [],
});

const runContext: RunContext = {
  project: project("storefront", "storefront"),
  access: { role: "maintainer", permissions: ["view_project", "run_test", "cancel_run"], custom: false },
  scenario: { ...scenario("checkout-flow", 3), id: "s" },
  executed: null,
  environment: environment("staging", true),
};

const challenge = {
  verification_status: "pending" as const,
  token: "c2b1a9e4f07d4b7f9a1e6d3c5b8a7f20",
  url: "https://staging.example.test/.well-known/ballast-c2b1a9e4f07d4b7f9a1e6d3c5b8a7f20",
  verified_at: "2026-10-11T10:00:00Z",
  verified_by: "0b6f1c1e-6d1b-4a63-8f6e-2b7d7f0a9c11",
};

const lastRun = (status: "passed" | "failed" | "running", endedAt?: string) => ({
  id: "54ce733c-2ecc-46b4-adbf-5aaed9a8e391",
  number: 1482,
  project_id: "checkout",
  scenario_id: "s",
  environment_id: "staging",
  scenario_version: 3,
  profile: "load" as const,
  params: { target_vus: 10, ramp_up_s: 30, plateau_s: 120, ramp_down_s: 30 },
  status,
  created_at: "2026-10-12T14:50:00Z",
  ended_at: endedAt,
});

const projectCards: ProjectOverview[] = [
  {
    project: project("storefront", "storefront", {
      infra: "k8s",
      region: "eu-west-1",
      owner: "Payments team",
      stack: ["go", "pg", "rds", "k6", "ngx", "rmq"],
      favorite: true,
    }),
    access: "full",
    environments: [environment("staging", true), environment("production", true), environment("preview", false)],
    lastRun: lastRun("passed", "2026-10-12T15:02:00Z"),
  },
  {
    project: project("identity", "identity", { infra: "railway", stack: ["node", "pg", "rds"] }),
    access: "full",
    environments: [environment("staging", true), environment("production", true)],
    lastRun: lastRun("failed", "2026-10-10T08:00:00Z"),
  },
  {
    project: project("search-index", "search-index", { infra: "vm", region: "bare metal", stack: ["rust", "es"] }),
    access: "full",
    environments: [environment("production", true)],
    lastRun: lastRun("running", "2026-10-11T08:00:00Z"),
  },
  // Nothing said about it: no chip, no blank, no "undefined".
  { project: project("Media pipeline", "media-pipeline"), access: "full", environments: [], lastRun: null },
  { project: project("Partner gateway", "partner-gateway", { infra: "railway" }), access: "restricted" },
];

// A trend's statistics, for the phase and endpoint tables below.
const trendOf = (metric: string, avg: number, tag?: string): Metric[] =>
  ([["avg", avg], ["med", avg * 0.9], ["p(95)", avg * 2.1], ["p(99)", avg * 3.4], ["max", avg * 6]] as const).map(
    ([stat, value]) => ({ metric, tag, stat, value }),
  );

const sampleMetrics: Metric[] = [
  ...trendOf("http_req_blocked", 0.4),
  ...trendOf("http_req_connecting", 1.2),
  ...trendOf("http_req_tls_handshaking", 3.8),
  ...trendOf("http_req_sending", 0.1),
  ...trendOf("http_req_waiting", 84.2),
  ...trendOf("http_req_receiving", 2.6),
  ...trendOf("http_req_duration", 120.5, "name:POST /login"),
  { metric: "http_reqs", tag: "name:POST /login", stat: "count", value: 1840 },
  { metric: "http_req_failed", tag: "name:POST /login", stat: "rate", value: 0.012 },
  ...trendOf("http_req_duration", 35.1, "expected_response:true"),
];

const rolesMatrix: RolesMatrix = {
  capabilities: [
    { id: "view_runs", label: "View runs and reports" },
    { id: "run_test", label: "Trigger a run" },
    { id: "edit_scenarios", label: "Edit scenarios" },
    { id: "manage_environments", label: "Manage environments" },
    { id: "verify_domain", label: "Verify a domain" },
    { id: "manage_members", label: "Manage members and roles" },
  ],
  roles: [
    { role: "viewer", capabilities: ["view_runs"] },
    { role: "runner", capabilities: ["view_runs", "run_test"] },
    { role: "maintainer", capabilities: ["view_runs", "run_test", "edit_scenarios", "manage_environments"] },
    {
      role: "admin",
      capabilities: ["view_runs", "run_test", "edit_scenarios", "manage_environments", "verify_domain", "manage_members"],
    },
  ],
};

const sampleMember = (id: string, name: string, role: Member["role"], last: string | null): Member => ({
  user_id: id,
  email: `${name.toLowerCase().replace(/ /g, ".")}@acme.dev`,
  name,
  role,
  has_custom_permissions: false,
  created_at: "2026-09-01T09:00:00Z",
  last_activity: last,
});

const sampleMembers: Member[] = [
  sampleMember("m1", "Maëlle Ferrand", "maintainer", "2026-10-18T11:56:00Z"),
  sampleMember("m2", "Tomas Reiner", "admin", "2026-10-17T10:00:00Z"),
  sampleMember("m3", "Jonas Berg", "viewer", "2026-10-16T10:00:00Z"),
  { ...sampleMember("m4", "Rin Takeda", "runner", null), name: "" },
];

const sampleInvitations: Invitation[] = [
  {
    id: "i1",
    email: "a.duval@acme.dev",
    role: "runner",
    state: "pending",
    expires_at: "2026-10-24T09:00:00Z",
    created_at: "2026-10-17T09:00:00Z",
  },
  {
    id: "i2",
    email: "e.hoffmann@acme.dev",
    role: "viewer",
    state: "expired",
    expires_at: "2026-10-15T09:00:00Z",
    created_at: "2026-10-08T09:00:00Z",
  },
];

const previewOf = (state: "pending" | "accepted" | "revoked" | "expired") => ({
  organization_name: "Acme",
  project_name: "storefront",
  email: "ada@acme.dev",
  role: "maintainer" as const,
  state,
  expires_at: "2026-10-24T09:00:00Z",
});

function Section({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <section className="flex flex-col gap-4">
      <h2 className="text-[11px] tracking-wider text-muted-foreground uppercase">{title}</h2>
      {children}
    </section>
  );
}

// ComponentReference shows every shared component in every state, so that a
// change to a token or a component can be judged in one place. It is reached
// at /dev/components, in development only.
export function ComponentReference() {
  return (
    <div className="flex flex-col gap-12">
      <header className="flex flex-col gap-2">
        <h1 className="text-3xl font-normal tracking-tight">Components</h1>
        <p className="text-sm text-muted-foreground">
          Development only. Every status must stay readable in grayscale, from its icon and its word.
        </p>
      </header>

      <Section title="Logo">
        <div className="flex flex-wrap items-end gap-6">
          <Logo className="size-4 text-brand" title="Ballast, 16 px" />
          <Logo className="size-8 text-brand" title="Ballast, 32 px" />
          <Logo className="size-16 text-brand" title="Ballast, 64 px" />
          <Logo className="size-16 text-foreground" title="Ballast, following the text colour" />
          <Logo className="size-16 text-muted-foreground" title="Ballast, muted" />
        </div>
      </Section>

      <Section title="Run status — pill">
        <div className="flex flex-wrap gap-2">
          {statuses.map((status) => (
            <RunStatusBadge key={status} status={status} />
          ))}
        </div>
      </Section>

      <Section title="Threshold verdict — inline">
        <div className="flex flex-wrap gap-6">
          <StatusBadge variant="inline" tone="passed" icon={CircleCheck} label="Passed" />
          <StatusBadge variant="inline" tone="failed" icon={CircleX} label="Failed" />
          <StatusBadge variant="inline" tone="warning" icon={TriangleAlert} label="Degraded" />
        </div>
      </Section>

      <Section title="Metric cards">
        <div className="grid grid-cols-2 gap-3.5 lg:grid-cols-4">
          <MetricCard label="p95 latency" value="1.69 ms" />
          <MetricCard label="p99 latency" value="2.37 ms" hint="threshold p(99)<800" />
          <MetricCard
            label="Error rate"
            value="32.62 %"
            hint="threshold rate<0.01"
            status={{ tone: "failed", icon: CircleX, label: "Failed" }}
          />
          <MetricCard
            label="Requests / sec"
            value="1,214.4 req/s"
            status={{ tone: "passed", icon: CircleCheck, label: "Passed" }}
          />
        </div>
      </Section>

      <Section title="Project cards">
        <div className="grid grid-cols-1 gap-3.5 md:grid-cols-2">
          {projectCards.map((overview) => (
            <ProjectCard key={overview.project.id} overview={overview} now={now} />
          ))}
        </div>
      </Section>

      <Section title="Project list: toolbar, grid and list views">
        <p className="text-xs text-muted-foreground">
          Favourites first whatever the order. The grid, sorted by last status, then the list view.
        </p>
        <ProjectList overviews={projectCards} now={now} sort="status" view="grid" />
        <ProjectList overviews={projectCards} now={now} sort="name" view="list" />
        <ProjectList overviews={[]} now={now} />
      </Section>

      <Section title="Environments">
        <p className="text-xs text-muted-foreground">
          As somebody who manages environments: Edit once verified, Verify before, the window with its days.
        </p>
        <EnvironmentList
          projectId="checkout"
          canManage
          now={now}
          environments={[
            {
              ...environment("production", true),
              max_vus: 1000,
              effective_max_vus: 200,
              allowed_window: {
                start: "22:00",
                end: "04:00",
                timezone: "Europe/Paris",
                days: ["mon", "tue", "wed", "thu", "fri"],
              },
            },
            { ...environment("staging", true), allowed_window: { start: "22:00", end: "06:00", timezone: "Europe/Paris" } },
            {
              ...environment("partner-sandbox", false),
              max_vus: 100,
              effective_max_vus: 100,
              allowed_window: { start: "00:00", end: "23:59", timezone: "UTC", days: ["sat", "sun"] },
            },
            { ...environment("preview", false), verification_checked_at: "2026-10-18T11:57:00Z" },
            { ...environment("canary", false), verification_status: "proof_ok" },
            { ...environment("legacy", false), verification_status: "verification_failed" },
          ]}
        />
        <p className="text-xs text-muted-foreground">As anybody else: nothing the API would refuse is offered.</p>
        <EnvironmentList projectId="checkout" now={now} environments={[environment("preview", false), environment("staging", true)]} />
        <EnvironmentList projectId="checkout" now={now} environments={[]} />
      </Section>

      <Section title="Runners">
        <p className="text-xs text-muted-foreground">
          One of each: online, draining while a deployment lets its last run finish, offline after silence, and one
          below the minimum contract — still online, taking no run.
        </p>
        <RunnerCard
          now={now}
          list={{
            minimum_contract: 2,
            runners: [
              runner("vps-1", "online", 3),
              runner("vps-2", "draining", 6),
              runner("vps-3", "offline", 540),
              runner("vps-4", "online", 8, 1),
            ],
          }}
        />
        <p className="text-xs text-muted-foreground">Before any runner has reported.</p>
        <RunnerCard now={now} list={{ minimum_contract: 1, runners: [] }} />
      </Section>

      <Section title="Shell: sidebar, role and breadcrumb">
        <div className="flex flex-wrap items-start gap-4">
          {[false, true].map((collapsed) => (
            <div
              key={String(collapsed)}
              data-collapsed={collapsed}
              className={`group/sidebar flex flex-col gap-6 rounded-xl border bg-sidebar p-4 data-[collapsed=true]:w-14 data-[collapsed=true]:gap-3 data-[collapsed=true]:p-2 ${collapsed ? "" : "w-60"}`}
            >
              <p className="text-xs text-muted-foreground group-data-[collapsed=true]/sidebar:sr-only">
                {collapsed ? "Collapsed" : "Open"}
              </p>
              <NavGroup title="Workspace">
                <NavItem href="#" icon={FolderKanban}>
                  Projects
                </NavItem>
              </NavGroup>
              <NavGroup title="storefront">
                <NavItem href="#" icon={LayoutDashboard} active>
                  Overview
                </NavItem>
                <NavItem href="#" icon={Workflow}>
                  Scenarios
                </NavItem>
                <NavItem href="#" icon={Server}>
                  Environments
                </NavItem>
              </NavGroup>
              <NavGroup title="A project with a very long name that has to be cut">
                <NavItem href="#" icon={Users}>
                  Members
                </NavItem>
              </NavGroup>
              <div className="border-t pt-3">
                <UserMenu user={{ name: "youssef", email: "youssef@example.test" }} role="maintainer" />
                {!collapsed && <UserMenu user={{ name: "youssef", email: "youssef@example.test" }} />}
              </div>
            </div>
          ))}
        </div>
        {shellPaths.map((path) => (
          <div key={path} className="flex flex-col gap-1">
            <span className="data text-[11px] text-muted-foreground">{path}</span>
            <Breadcrumb trail={buildTrail(path, "acme", path === "/projects" ? null : shellScope)} />
          </div>
        ))}
      </Section>

      <Section title="Scenarios">
        <ScenarioList
          projectId="checkout"
          scenarios={[
            { ...scenario("checkout", 3), threshold_count: 2 },
            { ...scenario("search", 1), threshold_count: 1 },
          ]}
        />
        <ScenarioList projectId="checkout" scenarios={[]} />
      </Section>

      <Section title="Script editor and findings">
        <ScriptEditor value={starterScript} marked={new Set([10])} invalid />
        <ScriptFindings
          kind="error"
          findings={[
            {
              code: "hardcoded_url",
              message: "the script addresses a host of its own instead of reading __ENV.BASE_URL",
              detail: "https://shop.example.test/login",
              line: 10,
            },
            { code: "declares_options", message: "the script exports its own options", line: 4 },
          ]}
        />
        <ScriptFindings
          kind="warning"
          findings={[{ code: "no_request_name_tags", message: "the script makes 2 requests and tags none of them with a name" }]}
        />
        <ScriptEditor value={starterScript} readOnly />
      </Section>

      <Section title="Thresholds">
        <ThresholdRules projectId="checkout" scenarioId="checkout" thresholds={thresholds} editable />
        <ThresholdRules projectId="checkout" scenarioId="checkout" thresholds={thresholds} editable={false} />
        <ThresholdRules projectId="checkout" scenarioId="checkout" thresholds={[]} editable={false} />
      </Section>

      <Section title="Environment verification">
        {(["pending", "verification_failed", "proof_ok", "verified"] as const).map((status) => (
          <VerificationPanel
            key={status}
            projectId="checkout"
            environment={{ ...environment("staging", false), verification_status: status }}
            challenge={challenge}
            canVerify
            canApprove
          />
        ))}
        <VerificationPanel
          projectId="checkout"
          environment={{ ...environment("staging", false), verification_status: "proof_ok" }}
          challenge={null}
          canVerify={false}
          canApprove={false}
        />
      </Section>

      <Section title="Environment secrets">
        <SecretList
          projectId="checkout"
          environmentId="staging"
          secrets={[
            { id: "s1", key: "API_TOKEN", created_at: "2026-10-01T09:00:00Z", updated_at: "2026-10-10T14:30:00Z" },
            { id: "s2", key: "ADMIN_PASSWORD", created_at: "2026-10-02T09:00:00Z", updated_at: "2026-10-02T09:00:00Z" },
          ]}
        />
        <SecretList projectId="checkout" environmentId="staging" secrets={[]} />
      </Section>

      <Section title="Members: permissions matrix and table">
        <PermissionsMatrix matrix={rolesMatrix} />
        <p className="text-xs text-muted-foreground">
          As an admin: every role can be changed but the last admin&apos;s, which says why.
        </p>
        <MembersTable
          projectId="checkout"
          members={sampleMembers}
          caller={{ canManage: true, role: "admin" }}
          now={new Date("2026-10-18T12:00:00Z")}
        />
        <p className="text-xs text-muted-foreground">As a maintainer: every role is locked, and there is no menu.</p>
        <MembersTable
          projectId="checkout"
          members={sampleMembers}
          caller={{ canManage: false, role: "maintainer" }}
          now={new Date("2026-10-18T12:00:00Z")}
        />
      </Section>

      <Section title="Members: invitations">
        <div>
          <InviteMember projectId="checkout" roles={["viewer", "runner", "maintainer", "admin"]} />
        </div>
        <PendingInvitations projectId="checkout" invitations={sampleInvitations} now={new Date("2026-10-18T12:00:00Z")} />
        <div className="grid gap-8 md:grid-cols-2">
          {(["pending", "accepted", "revoked", "expired"] as const).map((state) => (
            <InvitationCard
              key={state}
              token="example"
              invitation={previewOf(state)}
              signedInAs="ada@acme.dev"
              now={new Date("2026-10-18T12:00:00Z")}
            />
          ))}
          <InvitationCard
            token="example"
            invitation={previewOf("pending")}
            signedInAs="someone@else.dev"
            now={new Date("2026-10-18T12:00:00Z")}
          />
        </div>
      </Section>

      <Section title="Organization selector">
        <div className="max-w-xs pb-28">
          <OrgSelector
            organization={{ id: "o1", name: "Acme" }}
            organizations={[
              { id: "o1", name: "Acme" },
              { id: "o2", name: "Ada's organization" },
            ]}
          />
        </div>
      </Section>

      <Section title="Project page: tiles, banner and tabs">
        <ProjectTabs projectId="checkout" />
        <StatTiles tiles={tilesOf(sampleStats)} days={30} />
        <p className="text-xs text-muted-foreground">A window with no run, then one with nothing before it to compare with:</p>
        <StatTiles tiles={tilesOf(emptyStats)} days={30} />
        <StatTiles tiles={tilesOf(newStats)} days={30} />
        <RecentRun run={runRows[0]} now={now} />
        <ProjectPage
          project={project("checkout", "checkout")}
          access={{ role: "maintainer", permissions: ["view_project", "run_test", "manage_scenarios"], custom: false }}
          scenarios={[]}
          environments={[]}
          stats={emptyStats}
          rows={[]}
        />
      </Section>

      <Section title="Run page: header, tabs and output">
        {(["failed", "running", "passed"] as const).map((status) => (
          <RunHeader key={status} run={sampleRun(status)} context={runContext} now={sampleNow} />
        ))}
        <RerunRun runId="r1" scenario="checkout-flow" version={3} profile="load" load="400 VUs · 60s / 360s / 60s" />
        <RunTabs
          initial="output"
          summary={<p className="text-sm text-muted-foreground">The summary: figures, thresholds, phases, endpoints.</p>}
          output={
            <div className="data overflow-hidden rounded-xl border bg-background py-2 text-xs leading-5">
              <LogLine seq={412} time="14:06:02" level={null}>
                INFO  scenario=checkout-flow stage=add-to-cart vus=400
              </LogLine>
              <LogLine seq={415} time="14:06:03" level="warn">
                threshold http_req_duration p(95)=812ms approaching limit 800ms
              </LogLine>
              <LogLine seq={418} time="14:06:06" level="error">
                POST /api/checkout 503 in 4 021ms body=&quot;upstream connect error&quot;
              </LogLine>
              <LogLine seq={419} time="14:06:06" level={null}>
                INFO  retrying checkout for vu=118 attempt=2
              </LogLine>
            </div>
          }
        />
      </Section>

      <Section title="Runs table, filters and pages">
        <RunFilterBar
          projectId="checkout"
          filters={{ status: "failed" }}
          scenarios={[{ id: "s1", name: "checkout-flow" }]}
          environments={[{ id: "e1", name: "staging" }]}
        />
        <RunsTable rows={runRows} />
        <RunsPage
          project={project("checkout", "checkout")}
          access={{ role: "runner", permissions: ["view_project", "run_test"], custom: false }}
          page={runsPage({ rows: runRows, next: "n", prev: "p", inHistory: true })}
        />
        <RunsPage
          project={project("checkout", "checkout")}
          access={{ role: "runner", permissions: ["view_project", "run_test"], custom: false }}
          page={runsPage({ rows: [], filters: { status: "failed" } })}
        />
        <RunsPage
          project={project("checkout", "checkout")}
          access={{ role: "runner", permissions: ["view_project", "run_test"], custom: false }}
          page={runsPage({ rows: [] })}
        />
      </Section>

      <Section title="HTTP phases and endpoints">
        <HttpPhases metrics={sampleMetrics} results="reported" />
        <EndpointMetrics metrics={sampleMetrics} results="reported" />
        <EndpointMetrics metrics={[]} results="reported" />
        <EndpointMetrics metrics={[]} results="pending" />
      </Section>

      <Section title="Numbers in columns">
        <table className="w-full max-w-xl text-sm">
          <tbody>
            {latencies.map(([name, ...values]) => (
              <tr key={name} className="border-b">
                <td className="py-1.5">{name}</td>
                {values.map((value) => (
                  <td key={value} className="num py-1.5 text-right">
                    {value}
                  </td>
                ))}
              </tr>
            ))}
          </tbody>
        </table>
      </Section>

      <Section title="Buttons">
        <div className="flex flex-wrap items-center gap-2">
          <Button>Primary</Button>
          <Button variant="secondary">Secondary</Button>
          <Button variant="outline">Outline</Button>
          <Button variant="ghost">Discreet</Button>
          <Button variant="destructive">
            <Trash2 aria-hidden />
            Delete project
          </Button>
          <Button disabled>Disabled</Button>
        </div>
        <p className="text-xs text-muted-foreground">
          Destructive is not red: red means a failed run. It says what it does, and asks to confirm.
        </p>
      </Section>

      <Section title="Typography">
        <div className="flex flex-col gap-1">
          <p className="text-sm font-normal">Interface text, weight 400</p>
          <p className="text-sm font-medium">Emphasis, weight 500</p>
          <p className="data text-sm break-all">54ce733c-2ecc-46b4-adbf-5aaed9a8e391 · https://api.example.test/health</p>
        </div>
      </Section>

      <Section title="Loading, error and empty states">
        <LoadingState label="Loading the run…" />
        <p className="text-xs text-muted-foreground">
          Skeletons stand where the content will be, in the shape of the page. One status for a screen reader.
        </p>
        <SkeletonPage label="Loading the projects…">
          <SkeletonHeading />
          <SkeletonCards count={3} />
          <SkeletonTiles />
          <SkeletonTable rows={3} />
          <SkeletonPanel height="h-24" />
          <Skeleton className="h-4 w-40" />
        </SkeletonPage>
        <ErrorStateSample />
        <EmptyState title="No runs yet" description="Runs appear here once a scenario has been executed." />
      </Section>

      <Section title="Dates: one format, the reader's zone, the full date in a tooltip">
        <ul className="data flex flex-col gap-1 text-sm">
          <li>
            moment: <Time iso="2026-10-16T12:51:19Z" />
          </li>
          <li>
            relative: <Time iso="2026-10-18T09:00:00Z" mode="relative" now={now} />
          </li>
          <li>
            clock, today: <Time iso="2026-10-18T11:51:19Z" mode="clock" now={now} />
          </li>
          <li>
            clock, another day: <Time iso="2026-10-16T12:51:19Z" mode="clock" now={now} />
          </li>
          <li>
            nothing to say: <Time iso={undefined} />
          </li>
        </ul>
      </Section>

      <Section title="Scenario: top bar with target and Run">
        <p className="text-xs text-muted-foreground">
          Somebody who may edit and run, with two verified targets. Edit the script to see Run held back and why.
        </p>
        <ScenarioWorkspace
          projectId="checkout"
          projectName="storefront"
          scenario={{
            id: "s1",
            name: "checkout-flow",
            version: 3,
            default_profile: "load",
            default_params: { target_vus: 400, ramp_up_s: 60, plateau_s: 360, ramp_down_s: 60 },
            created_at: "2026-10-01T09:00:00Z",
            updated_at: "2026-10-18T11:58:00Z",
            script: starterScript,
            mode: "code",
            warnings: [],
          }}
          targets={[
            { id: "e1", name: "staging-eu" },
            { id: "e2", name: "staging-us" },
          ]}
          canEdit
          canRun
          now={now}
        />
        <p className="text-xs text-muted-foreground">Read-only, with no verified environment: Run says why.</p>
        <ScenarioWorkspace
          projectId="checkout"
          projectName="storefront"
          scenario={{
            id: "s2",
            name: "search",
            version: 1,
            default_profile: "smoke",
            default_params: { target_vus: 0, ramp_up_s: 0, plateau_s: 0, ramp_down_s: 0 },
            created_at: "2026-10-01T09:00:00Z",
            updated_at: "2026-10-10T09:00:00Z",
            script: starterScript,
            mode: "code",
            warnings: [],
          }}
          targets={[]}
          canEdit={false}
          canRun
          now={now}
        />
      </Section>
      <Section title="New scenario: code or graph">
        <NewScenarioPage project={project("storefront", "checkout")} endpoints={[]} start="graph" />
      </Section>

      <Section title="Builder: problems and script preview">
        <BuilderSamples />
      </Section>

      <Section title="Scenario builder">
        <p className="text-xs text-muted-foreground">
          Every kind of node, with one step left unwired to show its marker. Drag from the palette, press Enter on a
          palette entry, or select a node to edit it.
        </p>
        <Builder
          projectId="checkout"
          projectName="storefront"
          scenario={graphScenario("checkout-flow", sampleGraph)}
          targets={[{ id: "e1", name: "staging-eu", effectiveVus: 100, effectiveSeconds: 600 }]}
          thresholds={sampleThresholds}
          canEdit
          canRun
          now={now}
          endpoints={sampleCatalog}
          secretKeys={{ e1: ["LOGIN_EMAIL", "CLIENT_SECRET"] }}
        />
        <p className="text-xs text-muted-foreground">Read-only, for a viewer.</p>
        <Builder
          projectId="checkout"
          projectName="storefront"
          scenario={graphScenario("checkout-flow", sampleGraph)}
          targets={[]}
          thresholds={sampleThresholds}
          canEdit={false}
          canRun={false}
          now={now}
        />
        <p className="text-xs text-muted-foreground">Empty canvas.</p>
        <Builder
          projectId="checkout"
          projectName="storefront"
          scenario={graphScenario("new-flow", { start: "", nodes: [], edges: [] })}
          targets={[{ id: "e1", name: "staging-eu", effectiveVus: 100, effectiveSeconds: 600 }]}
          thresholds={[]}
          canEdit
          canRun
          now={now}
        />
      </Section>
    </div>
  );
}
