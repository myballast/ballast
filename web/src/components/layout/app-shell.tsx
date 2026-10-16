import { Menu } from "lucide-react";

import type { Me } from "@/features/auth/api";
import type { ShellScope } from "@/features/shell/types";

import { Brand } from "@/components/brand/brand";
import { AppHeader } from "./app-header";
import { Nav } from "./nav";
import { OrgSelector } from "./org-selector";
import { UserMenu } from "./user-menu";

// On narrow screens the brand is already in the top bar the menu opens from.
function SidebarContent({ me, scope, brand = true }: { me: Me; scope: ShellScope | null; brand?: boolean }) {
  return (
    <div className="flex h-full flex-col gap-6 p-4">
      {brand && <Brand />}
      <OrgSelector organization={me.organization} />
      <Nav project={scope?.project} />
      <div className="mt-auto border-t pt-3">
        <UserMenu user={me.user} role={scope?.role} />
      </div>
    </div>
  );
}

// AppShell is the frame of every signed-in page: a sidebar on wide screens, a
// top bar with a menu on narrow ones. The narrow menu is a <details>, so it
// works without JavaScript.
//
// scope is the project the page belongs to, if it does: its section of the
// sidebar, the role in the footer and the names in the breadcrumb.
export function AppShell({
  me,
  scope = null,
  children,
}: {
  me: Me;
  scope?: ShellScope | null;
  children: React.ReactNode;
}) {
  return (
    <div className="flex min-h-svh flex-1">
      <aside className="sticky top-0 hidden h-svh w-sidebar shrink-0 border-r bg-sidebar text-sidebar-foreground md:block">
        <SidebarContent me={me} scope={scope} />
      </aside>

      <div className="flex min-w-0 flex-1 flex-col">
        <details className="group border-b bg-sidebar md:hidden">
          <summary className="flex cursor-pointer list-none items-center justify-between px-4 py-3 [&::-webkit-details-marker]:hidden">
            <Brand />
            <Menu aria-hidden className="size-5 text-muted-foreground" />
            <span className="sr-only">Menu</span>
          </summary>
          <div className="border-t">
            <SidebarContent me={me} scope={scope} brand={false} />
          </div>
        </details>

        <AppHeader organization={me.organization.name} scope={scope} />
        <main className="mx-auto w-full max-w-6xl px-4 py-page-y md:px-page-x">{children}</main>
      </div>
    </div>
  );
}
