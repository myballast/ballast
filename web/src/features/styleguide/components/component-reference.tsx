import { CircleCheck, CircleX, Trash2, TriangleAlert } from "lucide-react";

import { Logo } from "@/components/brand/logo";
import { MetricCard } from "@/components/metric-card";
import { EmptyState, LoadingState } from "@/components/states";
import { StatusBadge } from "@/components/status-badge";
import { Button } from "@/components/ui/button";
import { EnvironmentList } from "@/features/environments/components/environment-list";
import { SecretList } from "@/features/environments/components/secret-list";
import { ScenarioList } from "@/features/scenarios/components/scenario-list";
import { ScriptEditor } from "@/features/scenarios/components/script-editor";
import { ScriptFindings } from "@/features/scenarios/components/script-findings";
import { ThresholdRules } from "@/features/scenarios/components/threshold-rules";
import { starterScript } from "@/features/scenarios/template";
import { VerificationPanel } from "@/features/environments/components/verification-panel";
import { ProjectCard } from "@/features/projects/components/project-card";
import { EndpointMetrics } from "@/features/runs/components/endpoint-metrics";
import { HttpPhases } from "@/features/runs/components/http-phases";
import { RunHistory } from "@/features/runs/components/run-history";
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

const project = (name: string, slug: string) => ({
  id: slug,
  name,
  slug,
  created_at: "2026-10-01T09:00:00Z",
});

const environment = (name: string, verified: boolean) => ({
  id: name,
  name,
  base_url: `https://${name}.example.test`,
  verification_status: verified ? ("verified" as const) : ("pending" as const),
  max_vus: 50,
  max_duration_s: 600,
  created_at: "2026-10-01T09:00:00Z",
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

const challenge = {
  verification_status: "pending" as const,
  token: "c2b1a9e4f07d4b7f9a1e6d3c5b8a7f20",
  url: "https://staging.example.test/.well-known/ballast-c2b1a9e4f07d4b7f9a1e6d3c5b8a7f20",
  verified_at: "2026-10-11T10:00:00Z",
  verified_by: "0b6f1c1e-6d1b-4a63-8f6e-2b7d7f0a9c11",
};

const lastRun = (status: "passed" | "failed" | "running", endedAt?: string) => ({
  id: "54ce733c-2ecc-46b4-adbf-5aaed9a8e391",
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
    project: project("Checkout service", "checkout"),
    access: "full",
    environments: [environment("staging", true), environment("production", true), environment("preview", false)],
    lastRun: lastRun("passed", "2026-10-12T15:02:00Z"),
  },
  {
    project: project("Search", "search"),
    access: "full",
    environments: [environment("staging", true)],
    lastRun: lastRun("failed", "2026-10-10T08:00:00Z"),
  },
  { project: project("Media pipeline", "media-pipeline"), access: "full", environments: [], lastRun: null },
  { project: project("Partner gateway", "partner-gateway"), access: "restricted" },
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

      <Section title="Environments">
        <EnvironmentList
          projectId="checkout"
          environments={[
            { ...environment("staging", true), allowed_window: { start: "22:00", end: "06:00", timezone: "Europe/Paris" } },
            environment("preview", false),
            { ...environment("canary", false), verification_status: "proof_ok" },
            { ...environment("legacy", false), verification_status: "verification_failed" },
          ]}
        />
        <EnvironmentList projectId="checkout" environments={[]} />
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

      <Section title="Run history">
        <RunHistory
          projectId="checkout"
          names={{ scenarios: {}, environments: {} }}
          first={{
            rows: (["passed", "failed", "running"] as const).map((status, i) => ({
              id: `run-${i}`,
              status,
              scenario: "checkout",
              environment: "staging",
              profile: "load",
              createdAt: `12 Oct 2026, 1${i}:00:00`,
            })),
          }}
        />
        <RunHistory projectId="checkout" names={{ scenarios: {}, environments: {} }} first={{ rows: [] }} />
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

      <Section title="Loading and empty states">
        <LoadingState label="Loading the run…" />
        <EmptyState title="No runs yet" description="Runs appear here once a scenario has been executed." />
      </Section>
    </div>
  );
}
