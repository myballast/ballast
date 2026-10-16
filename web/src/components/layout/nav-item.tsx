import Link from "next/link";

import { cn } from "@/lib/utils";

// NavItem is one link of the sidebar. The current page is tinted in the
// brand's violet, with a violet border and dot: it says where you are, which
// is not a status. Nothing here is green, amber or red.
export function NavItem({
  href,
  active = false,
  children,
}: {
  href: string;
  active?: boolean;
  children: React.ReactNode;
}) {
  return (
    <Link
      href={href}
      aria-current={active ? "page" : undefined}
      className={cn(
        "flex h-[30px] items-center gap-2.5 rounded-lg border border-transparent px-2 text-[12.5px] text-muted-foreground hover:bg-sidebar-accent hover:text-foreground",
        active && "border-brand/30 bg-brand/15 text-foreground hover:bg-brand/15",
      )}
    >
      <i aria-hidden className={cn("size-[5px] shrink-0 rounded-[1.5px] bg-muted-foreground/50", active && "bg-brand")} />
      {children}
    </Link>
  );
}

// NavGroup is a titled run of links. The title of a project's group is the
// project's name, which can be long: it is cut, and said in full on hover.
export function NavGroup({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <div className="flex flex-col gap-0.5">
      <p title={title} className="truncate px-2 pb-2 text-[11px] tracking-wider text-muted-foreground uppercase">
        {title}
      </p>
      {children}
    </div>
  );
}
