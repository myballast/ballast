import { CircleCheck, CircleX, Trash2, TriangleAlert } from "lucide-react";

import { Logo } from "@/components/brand/logo";
import { MetricCard } from "@/components/metric-card";
import { EmptyState, LoadingState } from "@/components/states";
import { StatusBadge } from "@/components/status-badge";
import { Button } from "@/components/ui/button";
import { ProjectCard } from "@/features/projects/components/project-card";
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

const lastRun = (status: "passed" | "failed" | "running", endedAt?: string) => ({
  id: "54ce733c-2ecc-46b4-adbf-5aaed9a8e391",
  project_id: "checkout",
  scenario_id: "s",
  environment_id: "staging",
  scenario_version: 3,
  profile: "load",
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
