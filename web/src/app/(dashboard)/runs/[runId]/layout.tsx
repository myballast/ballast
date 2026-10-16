import { AppShell } from "@/components/layout/app-shell";
import { getMe } from "@/features/auth/api";
import { loadRunScope } from "@/features/shell/load";

// A run's address carries no project, so the run is read to find it: the page
// of a run has the same shell as the pages of its project.
export default async function RunLayout(props: LayoutProps<"/runs/[runId]">) {
  const { runId } = await props.params;
  const [me, scope] = await Promise.all([getMe(), loadRunScope(runId)]);

  return (
    <AppShell me={me} scope={scope}>
      {props.children}
    </AppShell>
  );
}
