import type { LucideIcon } from "lucide-react";
import Link from "next/link";

import { cn } from "@/lib/utils";

// NavItem is one link of the sidebar. The current page is tinted in the
// brand's violet, with a violet border and icon: it says where you are, which
// is not a status. Nothing here is green, amber or red.
//
// When the sidebar is collapsed the word is still there for a screen reader and
// is said on hover, and the icon is all that is drawn.
export function NavItem({
  href,
  icon: Icon,
  active = false,
  children,
}: {
  href: string;
  icon: LucideIcon;
  active?: boolean;
  children: string;
}) {
  return (
    <Link
      href={href}
      aria-current={active ? "page" : undefined}
      title={children}
      className={cn(
        "flex h-8 items-center gap-2.5 rounded-lg border border-transparent px-2 text-[12.5px] text-muted-foreground hover:bg-sidebar-accent hover:text-foreground",
        "group-data-[collapsed=true]/sidebar:justify-center group-data-[collapsed=true]/sidebar:px-0",
        active && "border-brand/30 bg-brand/15 text-foreground hover:bg-brand/15",
      )}
    >
      <Icon aria-hidden className={cn("size-4 shrink-0", active && "text-brand")} />
      <span className="truncate group-data-[collapsed=true]/sidebar:sr-only">{children}</span>
    </Link>
  );
}

// NavGroup is a titled run of links. The title of a project's group is the
// project's name, which can be long: it is cut, and said in full on hover.
export function NavGroup({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <div className="flex flex-col gap-0.5 group-data-[collapsed=true]/sidebar:border-t group-data-[collapsed=true]/sidebar:pt-2 group-data-[collapsed=true]/sidebar:first:border-t-0 group-data-[collapsed=true]/sidebar:first:pt-0">
      <p
        title={title}
        className="truncate px-2 pb-2 text-[11px] tracking-wider text-muted-foreground uppercase group-data-[collapsed=true]/sidebar:sr-only"
      >
        {title}
      </p>
      {children}
    </div>
  );
}
