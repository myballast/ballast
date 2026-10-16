import { AppShell } from "@/components/layout/app-shell";
import { getMe } from "@/features/auth/api";
import { loadProjectScope } from "@/features/shell/load";

// Every page of a project shares one shell: the same section of the sidebar,
// the same role in the footer, the same names in the breadcrumb. A project that
// is not there leaves the plain shell, and the page answers 404 inside it.
export default async function ProjectLayout(props: LayoutProps<"/projects/[projectId]">) {
  const { projectId } = await props.params;
  const [me, scope] = await Promise.all([getMe(), loadProjectScope(projectId)]);

  return (
    <AppShell me={me} scope={scope}>
      {props.children}
    </AppShell>
  );
}
