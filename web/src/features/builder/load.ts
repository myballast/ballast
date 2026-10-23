import { getSecrets } from "@/features/environments/api";
import { loadScenarioPage, type ScenarioPage } from "@/features/scenarios/detail";
import { can } from "@/lib/permissions";

export type BuilderData = ScenarioPage & {
  // The keys of each verified environment's secrets. Never a value: the API has no
  // way to give one. Undefined when the caller may not list them, which takes the
  // right to manage environments.
  secretKeys?: Record<string, string[]>;
};

// loadBuilderPage reads what the builder shows: the scenario page's own data, and
// the names of the secrets a request can be bound to. A failure to list them is
// not a failure of the page — the suggestions simply have fewer entries.
export async function loadBuilderPage(projectId: string, scenarioId: string): Promise<BuilderData> {
  const page = await loadScenarioPage(projectId, scenarioId);
  if (!can(page.access, "manage_environments")) return page;

  const verified = page.environments.filter((environment) => environment.verification_status === "verified");
  const lists = await Promise.all(
    verified.map(async (environment) => {
      try {
        const secrets = await getSecrets(projectId, environment.id);
        return [environment.id, secrets.map((secret) => secret.key)] as const;
      } catch {
        return [environment.id, []] as const;
      }
    }),
  );

  return { ...page, secretKeys: Object.fromEntries(lists) };
}
