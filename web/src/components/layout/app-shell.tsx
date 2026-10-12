import { Menu } from "lucide-react";

import type { Me } from "@/features/auth/api";

import { Brand } from "@/components/brand/brand";
import { AppHeader } from "./app-header";
import { Nav } from "./nav";
import { OrgSelector } from "./org-selector";
import { UserMenu } from "./user-menu";

// On narrow screens the brand is already in the top bar the menu opens from.
function SidebarContent({ me, brand = true }: { me: Me; brand?: boolean }) {
  return (
    <div className="flex h-full flex-col gap-6 p-4">
      {brand && <Brand />}
      <OrgSelector organization={me.organization} />
      <Nav />
      <div className="mt-auto border-t pt-3">
        <UserMenu user={me.user} />
      </div>
    </div>
  );
}

// AppShell is the frame of every signed-in page: a sidebar on wide screens, a
// top bar with a menu on narrow ones. The narrow menu is a <details>, so it
// works without JavaScript.
export function AppShell({ me, children }: { me: Me; children: React.ReactNode }) {
  return (
    <div className="flex min-h-svh flex-1">
      <aside className="sticky top-0 hidden h-svh w-sidebar shrink-0 border-r bg-sidebar text-sidebar-foreground md:block">
        <SidebarContent me={me} />
      </aside>

      <div className="flex min-w-0 flex-1 flex-col">
        <details className="group border-b bg-sidebar md:hidden">
          <summary className="flex cursor-pointer list-none items-center justify-between px-4 py-3 [&::-webkit-details-marker]:hidden">
            <Brand />
            <Menu aria-hidden className="size-5 text-muted-foreground" />
            <span className="sr-only">Menu</span>
          </summary>
          <div className="border-t">
            <SidebarContent me={me} brand={false} />
          </div>
        </details>

        <AppHeader />
        <main className="mx-auto w-full max-w-6xl px-4 py-page-y md:px-page-x">{children}</main>
      </div>
    </div>
  );
}
