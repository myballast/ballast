import { AppShell } from "@/components/layout/app-shell";
import { getMe } from "@/features/auth/api";

// The project list is the workspace: the shell with no project in it. Without
// a session, getMe sends the browser to the login page before anything renders.
export default async function WorkspaceLayout({ children }: LayoutProps<"/projects">) {
  return <AppShell me={await getMe()}>{children}</AppShell>;
}
