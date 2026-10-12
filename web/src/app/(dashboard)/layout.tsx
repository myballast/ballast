import { AppShell } from "@/components/layout/app-shell";
import { getMe } from "@/features/auth/api";

// Every page in this group is signed in: without a session, getMe sends the
// browser to the login page before anything renders.
export default async function DashboardLayout({ children }: LayoutProps<"/">) {
  const me = await getMe();
  return <AppShell me={me}>{children}</AppShell>;
}
